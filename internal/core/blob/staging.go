package blob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// A resumable upload spans one or more requests, so its bytes must survive
// between them and be re-hashable at commit time. Both blob drivers stage an
// in-progress upload as a local temp file — writing, resuming, and hashing are
// identical regardless of where a committed blob comes to rest. The drivers
// differ only in the finalize step: the fs driver renames the temp file into its
// content-addressable tree, while the s3 driver uploads it to the bucket. This
// shared staging area holds that common machinery so the two drivers stay in
// lockstep (the driver-agnostic contract test enforces it).

// uploadIDPattern constrains upload ids to what we generate (hex), so an id
// taken from a URL can never escape the staging directory.
var uploadIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// finalizeFunc moves a verified upload's temp file into permanent storage and
// returns its descriptor. It owns the temp file from this point: on success it
// consumes it (rename or upload-then-remove), and the staging area does not touch
// it afterward.
type finalizeFunc func(ctx context.Context, tempPath, digest string, size int64) (Descriptor, error)

// stagingArea manages resumable uploads as local temp files under dir.
type stagingArea struct {
	dir string
}

// newStagingArea ensures the staging directory exists.
func newStagingArea(dir string) (*stagingArea, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating upload dir %s: %w", dir, err)
	}
	return &stagingArea{dir: dir}, nil
}

func (a *stagingArea) path(id string) string { return filepath.Join(a.dir, id) }

// start begins a new upload session whose commit runs finalize.
func (a *stagingArea) start(finalize finalizeFunc) (*stagedUpload, error) {
	id, err := newUploadID()
	if err != nil {
		return nil, err
	}
	// path is derived from a freshly generated hex id, not from user input.
	path := a.path(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_EXCL, 0o600) //nolint:gosec // id is server-generated hex
	if err != nil {
		return nil, fmt.Errorf("creating upload %s: %w", id, err)
	}
	return &stagedUpload{area: a, id: id, path: path, file: f, finalize: finalize}, nil
}

// resume reopens an existing session by id. The id is validated against a strict
// hex pattern before it is used in a path, so it cannot escape the staging dir.
func (a *stagingArea) resume(id string, finalize finalizeFunc) (*stagedUpload, error) {
	if !uploadIDPattern.MatchString(id) {
		return nil, ErrUploadNotFound
	}
	path := a.path(id)
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
	return &stagedUpload{area: a, id: id, path: path, file: f, size: info.Size(), finalize: finalize}, nil
}

// stagedUpload is one resumable session backed by a temp file. It implements
// blob.Upload; the driver supplies finalize.
type stagedUpload struct {
	area     *stagingArea
	id       string
	path     string
	file     *os.File
	size     int64
	finalize finalizeFunc
}

func (u *stagedUpload) ID() string  { return u.id }
func (u *stagedUpload) Size() int64 { return u.size }

func (u *stagedUpload) Write(p []byte) (int, error) {
	n, err := u.file.Write(p)
	u.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("writing upload %s: %w", u.id, err)
	}
	return n, nil
}

func (u *stagedUpload) Close() error {
	if u.file == nil {
		return nil
	}
	err := u.file.Close()
	u.file = nil
	return err
}

func (u *stagedUpload) Abort(_ context.Context) error {
	_ = u.Close()
	if err := os.Remove(u.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("aborting upload %s: %w", u.id, err)
	}
	return nil
}

// Commit flushes the session, verifies it hashes to expectedDigest, then hands
// the temp file to the driver's finalize. A mismatch discards the temp file and
// returns ErrDigestMismatch.
func (u *stagedUpload) Commit(ctx context.Context, expectedDigest string) (Descriptor, error) {
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
	return u.finalize(ctx, u.path, digest, size)
}

// hashFile reopens the temp file read-only and streams it through algo.
func (u *stagedUpload) hashFile(algo string) (string, int64, error) {
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
