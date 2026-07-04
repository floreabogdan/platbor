package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store is an S3-compatible content-addressable store (AWS S3, MinIO, R2, and
// friends). Committed blobs live at <prefix>/blobs/<algo>/<hex> in the bucket;
// in-progress uploads are staged locally (like the fs driver) and flushed to the
// object store on commit. That staging keeps resumable uploads and digest
// verification identical to the fs driver — the object store only holds finished,
// content-addressed blobs.
//
// Because staging is node-local, an in-progress resumable upload must be resumed
// on the node that started it; committed blobs are shared across all nodes via
// the bucket. Single-node deployments are unaffected.
type S3Store struct {
	client  *minio.Client
	bucket  string
	prefix  string // key prefix without a trailing slash; "" for the bucket root
	staging *stagingArea
}

// S3Options configures an S3Store.
type S3Options struct {
	Endpoint   string // host[:port] of the S3 endpoint (e.g. s3.amazonaws.com, localhost:9000)
	Bucket     string // bucket that holds the blobs
	Region     string // region (optional for MinIO)
	AccessKey  string // access key id (empty → anonymous / ambient credentials)
	SecretKey  string // secret access key
	UseSSL     bool   // whether the endpoint speaks HTTPS
	Prefix     string // optional key prefix within the bucket
	StagingDir string // local directory for in-progress uploads
}

// NewS3 connects to the object store, ensures the bucket exists, and prepares the
// local staging area.
func NewS3(ctx context.Context, opts S3Options) (*S3Store, error) {
	if opts.Endpoint == "" || opts.Bucket == "" {
		return nil, errors.New("s3 blob store requires an endpoint and a bucket")
	}
	var creds *credentials.Credentials
	if opts.AccessKey != "" || opts.SecretKey != "" {
		creds = credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, "")
	} else {
		// No explicit keys: fall back to the ambient chain (env, IAM role, etc.).
		creds = credentials.NewIAM("")
	}
	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: opts.UseSSL,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to s3 endpoint %s: %w", opts.Endpoint, err)
	}

	exists, err := client.BucketExists(ctx, opts.Bucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket %s: %w", opts.Bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, opts.Bucket, minio.MakeBucketOptions{Region: opts.Region}); err != nil {
			return nil, fmt.Errorf("creating bucket %s: %w", opts.Bucket, err)
		}
	}

	staging, err := newStagingArea(opts.StagingDir)
	if err != nil {
		return nil, err
	}
	return &S3Store{
		client:  client,
		bucket:  opts.Bucket,
		prefix:  strings.Trim(opts.Prefix, "/"),
		staging: staging,
	}, nil
}

// key builds the object key for a digest: <prefix>/blobs/<algo>/<hex>.
func (s *S3Store) key(digest string) string {
	return path.Join(s.prefix, "blobs", digestAlgo(digest), digestHex(digest))
}

// blobsPrefix is the key prefix under which all blobs live, for listing.
func (s *S3Store) blobsPrefix() string {
	return path.Join(s.prefix, "blobs") + "/"
}

// Stat implements Store.
func (s *S3Store) Stat(ctx context.Context, digest string) (Descriptor, error) {
	if err := ValidateDigest(digest); err != nil {
		return Descriptor{}, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, s.key(digest), minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return Descriptor{}, ErrNotFound
		}
		return Descriptor{}, fmt.Errorf("stat blob %s: %w", digest, err)
	}
	return Descriptor{Digest: digest, Size: info.Size}, nil
}

// Open implements Store.
func (s *S3Store) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	if err := ValidateDigest(digest); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, s.key(digest), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("open blob %s: %w", digest, err)
	}
	// GetObject is lazy; the request is issued on the first Stat/Read, so probe
	// here to surface a missing blob as ErrNotFound before returning the reader.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open blob %s: %w", digest, err)
	}
	return obj, nil
}

// Delete implements Store. S3 deletes are idempotent, so a missing blob is not an
// error.
func (s *S3Store) Delete(ctx context.Context, digest string) error {
	if err := ValidateDigest(digest); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, s.key(digest), minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete blob %s: %w", digest, err)
	}
	return nil
}

// Walk implements Store. It lists every object under <prefix>/blobs/ and
// reconstructs each digest from its key; keys that are not a valid blob path
// (e.g. a stray object) are skipped.
func (s *S3Store) Walk(ctx context.Context, fn func(Info) error) error {
	prefix := s.blobsPrefix()
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("listing blobs: %w", obj.Err)
		}
		rel := strings.TrimPrefix(obj.Key, prefix) // "<algo>/<hex>"
		algo, hex, ok := strings.Cut(rel, "/")
		if !ok {
			continue
		}
		digest := algo + ":" + hex
		if ValidateDigest(digest) != nil {
			continue // not a blob object
		}
		if err := fn(Info{Digest: digest, Size: obj.Size, ModTime: obj.LastModified}); err != nil {
			return err
		}
	}
	return nil
}

// StartUpload implements Store.
func (s *S3Store) StartUpload(_ context.Context) (Upload, error) {
	return s.staging.start(s.finalize)
}

// ResumeUpload implements Store.
func (s *S3Store) ResumeUpload(_ context.Context, id string) (Upload, error) {
	return s.staging.resume(id, s.finalize)
}

// finalize uploads a verified staged blob to the object store (minio-go streams
// it as a multipart upload when large), then removes the temp file. If the blob
// already exists it is byte-identical, so the upload is skipped (idempotent).
func (s *S3Store) finalize(ctx context.Context, tempPath, digest string, size int64) (Descriptor, error) {
	key := s.key(digest)
	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err == nil {
		_ = os.Remove(tempPath)
		return Descriptor{Digest: digest, Size: size}, nil
	} else if !isNotFound(err) {
		return Descriptor{}, fmt.Errorf("checking blob %s: %w", digest, err)
	}
	if _, err := s.client.FPutObject(ctx, s.bucket, key, tempPath, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	}); err != nil {
		return Descriptor{}, fmt.Errorf("uploading blob %s: %w", digest, err)
	}
	_ = os.Remove(tempPath)
	return Descriptor{Digest: digest, Size: size}, nil
}

// isNotFound reports whether an S3 error means the object (or bucket) is absent.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound ||
		resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket"
}
