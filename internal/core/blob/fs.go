package blob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// FSStore is a filesystem-backed content-addressable store. Committed blobs live
// at {root}/blobs/<algo>/<ab>/<hex> (algo is sha256 or sha512); in-progress
// uploads are temp files under {root}/uploads/<id>. A commit renames the temp
// file into place, which is atomic on a single volume.
type FSStore struct {
	root string
}

// uploadIDPattern constrains upload ids to what we generate (hex), so an id
// taken from a URL can never escape the uploads directory.
var uploadIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// NewFS creates the store's directory layout under root and returns the store.
func NewFS(root string) (*FSStore, error) {
	for _, dir := range []string{filepath.Join(root, "blobs"), filepath.Join(root, "uploads")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating blob dir %s: %w", dir, err)
		}
	}
	return &FSStore{root: root}, nil
}

func (s *FSStore) blobPath(digest string) string {
	h := digestHex(digest)
	return filepath.Join(s.root, "blobs", digestAlgo(digest), h[:2], h)
}

func (s *FSStore) uploadPath(id string) string {
	return filepath.Join(s.root, "uploads", id)
}

// Stat implements Store.
func (s *FSStore) Stat(_ context.Context, digest string) (Descriptor, error) {
	if err := ValidateDigest(digest); err != nil {
		return Descriptor{}, err
	}
	info, err := os.Stat(s.blobPath(digest))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Descriptor{}, ErrNotFound
		}
		return Descriptor{}, fmt.Errorf("stat blob %s: %w", digest, err)
	}
	return Descriptor{Digest: digest, Size: info.Size()}, nil
}

// Open implements Store.
func (s *FSStore) Open(_ context.Context, digest string) (io.ReadCloser, error) {
	if err := ValidateDigest(digest); err != nil {
		return nil, err
	}
	f, err := os.Open(s.blobPath(digest))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open blob %s: %w", digest, err)
	}
	return f, nil
}

// Delete implements Store.
func (s *FSStore) Delete(_ context.Context, digest string) error {
	if err := ValidateDigest(digest); err != nil {
		return err
	}
	if err := os.Remove(s.blobPath(digest)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete blob %s: %w", digest, err)
	}
	return nil
}

// Walk implements Store. It walks {root}/blobs/<algo>/<ab>/<hex> for every
// supported algorithm, reconstructing each digest from its path. Files whose
// name is not a valid hex digest (e.g. a stray temp file) are skipped rather
// than reported as blobs.
func (s *FSStore) Walk(_ context.Context, fn func(Info) error) error {
	for _, algo := range supportedAlgos {
		algoRoot := filepath.Join(s.root, "blobs", algo)
		err := filepath.WalkDir(algoRoot, func(_ string, d os.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil // nothing committed under this algorithm yet
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			digest := algo + ":" + d.Name()
			if ValidateDigest(digest) != nil {
				return nil // not a blob file
			}
			info, err := d.Info()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil // removed concurrently
				}
				return fmt.Errorf("stat blob %s: %w", digest, err)
			}
			return fn(Info{Digest: digest, Size: info.Size(), ModTime: info.ModTime()})
		})
		if err != nil {
			return fmt.Errorf("walking blobs: %w", err)
		}
	}
	return nil
}

// StartUpload implements Store.
func (s *FSStore) StartUpload(_ context.Context) (Upload, error) {
	id, err := newUploadID()
	if err != nil {
		return nil, err
	}
	// path is derived from a freshly generated hex id, not from user input.
	path := s.uploadPath(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_EXCL, 0o600) //nolint:gosec // id is server-generated hex
	if err != nil {
		return nil, fmt.Errorf("creating upload %s: %w", id, err)
	}
	return &fsUpload{store: s, id: id, path: path, file: f}, nil
}

// ResumeUpload implements Store. The id is validated against a strict hex
// pattern before it is used in a path, so it cannot escape the uploads dir.
func (s *FSStore) ResumeUpload(_ context.Context, id string) (Upload, error) {
	if !uploadIDPattern.MatchString(id) {
		return nil, ErrUploadNotFound
	}
	path := s.uploadPath(id)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUploadNotFound
		}
		return nil, fmt.Errorf("stat upload %s: %w", id, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // id validated by uploadIDPattern above
	if err != nil {
		return nil, fmt.Errorf("opening upload %s: %w", id, err)
	}
	return &fsUpload{store: s, id: id, path: path, file: f, size: info.Size()}, nil
}

// fsUpload is one resumable session backed by a temp file.
type fsUpload struct {
	store *FSStore
	id    string
	path  string
	file  *os.File
	size  int64
}

func (u *fsUpload) ID() string  { return u.id }
func (u *fsUpload) Size() int64 { return u.size }

func (u *fsUpload) Write(p []byte) (int, error) {
	n, err := u.file.Write(p)
	u.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("writing upload %s: %w", u.id, err)
	}
	return n, nil
}

func (u *fsUpload) Close() error {
	if u.file == nil {
		return nil
	}
	err := u.file.Close()
	u.file = nil
	return err
}

func (u *fsUpload) Abort(_ context.Context) error {
	_ = u.Close()
	if err := os.Remove(u.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("aborting upload %s: %w", u.id, err)
	}
	return nil
}

func (u *fsUpload) Commit(_ context.Context, expectedDigest string) (Descriptor, error) {
	if err := ValidateDigest(expectedDigest); err != nil {
		return Descriptor{}, err
	}
	if err := u.Close(); err != nil {
		return Descriptor{}, fmt.Errorf("flushing upload %s: %w", u.id, err)
	}

	digest, size, err := u.hashFile(digestAlgo(expectedDigest))
	if err != nil {
		return Descriptor{}, err
	}
	if digest != expectedDigest {
		_ = os.Remove(u.path)
		return Descriptor{}, fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, digest, expectedDigest)
	}

	dest := u.store.blobPath(digest)
	// Content-addressed: if the blob already exists it is byte-identical, so the
	// upload is redundant — drop it and report success (idempotent).
	if _, err := os.Stat(dest); err == nil {
		_ = os.Remove(u.path)
		return Descriptor{Digest: digest, Size: size}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return Descriptor{}, fmt.Errorf("creating blob dir: %w", err)
	}
	if err := os.Rename(u.path, dest); err != nil {
		return Descriptor{}, fmt.Errorf("committing blob %s: %w", digest, err)
	}
	return Descriptor{Digest: digest, Size: size}, nil
}

// hashFile reopens the temp file read-only and streams it through the expected
// digest's algorithm.
func (u *fsUpload) hashFile(algo string) (string, int64, error) {
	f, err := os.Open(u.path)
	if err != nil {
		return "", 0, fmt.Errorf("reopening upload %s: %w", u.id, err)
	}
	defer func() { _ = f.Close() }()
	return digestReader(algo, f)
}

func newUploadID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating upload id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
