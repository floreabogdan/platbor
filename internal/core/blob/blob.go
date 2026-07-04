// Package blob is Platbor's content-addressable store. All binary content —
// image layers, package tarballs, chart archives — lives here keyed by its
// sha256 digest; format adapters keep only metadata plus digest references.
//
// Writes go through resumable upload sessions (the OCI spec requires them, and
// other formats benefit): start a session, append bytes across one or more
// requests, then commit against an expected digest. A commit that does not
// hash to the expected digest is rejected, so corruption cannot enter the store.
//
// Deletion exists only for garbage collection; adapters never delete inline,
// because blobs are shared and content-addressed.
package blob

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	// ErrNotFound means no blob exists for the given digest.
	ErrNotFound = errors.New("blob not found")
	// ErrUploadNotFound means no upload session exists for the given id.
	ErrUploadNotFound = errors.New("upload not found")
	// ErrDigestMismatch means committed content did not hash to the expected
	// digest; the content is rejected.
	ErrDigestMismatch = errors.New("digest mismatch")
	// ErrInvalidDigest means the digest string is malformed or uses an
	// unsupported algorithm.
	ErrInvalidDigest = errors.New("invalid digest")
)

// Descriptor identifies a stored blob by digest and size.
type Descriptor struct {
	Digest string // canonical "sha256:<hex>"
	Size   int64
}

// Info describes a stored blob during enumeration: its descriptor plus when
// it was last written, so garbage collection can spare blobs freshly uploaded
// but not yet referenced by a manifest.
type Info struct {
	Digest  string
	Size    int64
	ModTime time.Time
}

// Store is a content-addressable blob store. Implementations (fs, s3 later) are
// fully substitutable — the contract test suite enforces it.
type Store interface {
	// Stat returns the descriptor for a stored blob, or ErrNotFound.
	Stat(ctx context.Context, digest string) (Descriptor, error)
	// Open returns a reader over a stored blob's content. The caller closes it.
	Open(ctx context.Context, digest string) (io.ReadCloser, error)
	// Delete removes a blob. It is used only by garbage collection; a missing
	// blob is not an error.
	Delete(ctx context.Context, digest string) error
	// Walk calls fn for every committed blob, in no guaranteed order. Garbage
	// collection uses it to enumerate what is stored. If fn returns an error,
	// Walk stops and returns it.
	Walk(ctx context.Context, fn func(Info) error) error

	// StartUpload begins a new resumable upload session.
	StartUpload(ctx context.Context) (Upload, error)
	// ResumeUpload reopens an existing session by id, or ErrUploadNotFound.
	ResumeUpload(ctx context.Context, id string) (Upload, error)
}

// HealthChecker is an optional Store capability: a cheap reachability probe (the
// backing filesystem or bucket answers). Readiness checks use it; a store that
// does not implement it is assumed always reachable.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// UploadSweeper is an optional Store capability: remove abandoned in-progress
// upload sessions last written before cutoff, returning how many were removed.
// Resumable uploads that a client starts but never commits or aborts would
// otherwise leak staging files; a background sweep reclaims them.
type UploadSweeper interface {
	SweepUploads(ctx context.Context, cutoff time.Time) (int, error)
}

// Upload is an in-progress, resumable blob upload. A session spans one or more
// requests: write bytes, Close to persist progress, and later Commit or Abort.
// A single session must not be written by two goroutines concurrently.
type Upload interface {
	// Write appends bytes to the session.
	io.Writer
	// ID identifies the session; it appears in the upload URL.
	ID() string
	// Size is the number of bytes accumulated so far.
	Size() int64
	// Commit verifies the accumulated content hashes to expectedDigest and
	// moves it into the store, returning its descriptor. On a digest mismatch
	// it returns ErrDigestMismatch and the session is discarded.
	Commit(ctx context.Context, expectedDigest string) (Descriptor, error)
	// Close releases the underlying handle while preserving the session on disk
	// so it can be resumed.
	Close() error
	// Abort discards an incomplete session and its data.
	Abort(ctx context.Context) error
}
