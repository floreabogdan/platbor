package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FSStore is a filesystem-backed content-addressable store. Committed blobs live
// at {root}/blobs/<algo>/<ab>/<hex> (algo is sha256 or sha512); in-progress
// uploads are temp files under {root}/uploads/<id>, managed by the shared staging
// area. A commit renames the temp file into place, which is atomic on a single
// volume.
type FSStore struct {
	root    string
	staging *stagingArea
}

// NewFS creates the store's directory layout under root and returns the store.
func NewFS(root string) (*FSStore, error) {
	if err := os.MkdirAll(filepath.Join(root, "blobs"), 0o750); err != nil {
		return nil, fmt.Errorf("creating blob dir: %w", err)
	}
	staging, err := newStagingArea(filepath.Join(root, "uploads"))
	if err != nil {
		return nil, err
	}
	return &FSStore{root: root, staging: staging}, nil
}

func (s *FSStore) blobPath(digest string) string {
	h := digestHex(digest)
	return filepath.Join(s.root, "blobs", digestAlgo(digest), h[:2], h)
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
	return s.staging.start(s.finalize)
}

// ResumeUpload implements Store.
func (s *FSStore) ResumeUpload(_ context.Context, id string) (Upload, error) {
	return s.staging.resume(id, s.finalize)
}

// finalize moves a verified upload's temp file into the content-addressable tree
// by renaming it (atomic on a single volume). If the blob already exists it is
// byte-identical, so the upload is redundant — drop it and report success.
func (s *FSStore) finalize(_ context.Context, tempPath, digest string, size int64) (Descriptor, error) {
	dest := s.blobPath(digest)
	if _, err := os.Stat(dest); err == nil {
		_ = os.Remove(tempPath)
		return Descriptor{Digest: digest, Size: size}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return Descriptor{}, fmt.Errorf("creating blob dir: %w", err)
	}
	if err := os.Rename(tempPath, dest); err != nil {
		return Descriptor{}, fmt.Errorf("committing blob %s: %w", digest, err)
	}
	return Descriptor{Digest: digest, Size: size}, nil
}
