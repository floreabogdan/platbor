package blob_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/platbor/platbor/internal/core/blob"
)

// sha512Digest is the canonical "sha512:<hex>" digest of data, used to exercise
// the store with a non-default algorithm.
func sha512Digest(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512:" + hex.EncodeToString(sum[:])
}

// runStoreContract exercises the behavior every blob.Store must exhibit. It is
// driver-agnostic: the fs suite runs it today, and the future s3 suite will run
// the exact same cases (the Liskov enforcer from docs/CODING-STANDARDS.md).
func runStoreContract(t *testing.T, newStore func(t *testing.T) blob.Store) {
	ctx := context.Background()

	t.Run("commit then stat and open", func(t *testing.T) {
		s := newStore(t)
		content := []byte("hello platbor")
		desc := putBlob(t, s, content)

		if desc.Digest != blob.DigestBytes(content) || desc.Size != int64(len(content)) {
			t.Fatalf("descriptor = %+v", desc)
		}
		got, err := s.Stat(ctx, desc.Digest)
		if err != nil || got != desc {
			t.Fatalf("Stat = %+v, %v", got, err)
		}
		if data := readBlob(t, s, desc.Digest); !bytes.Equal(data, content) {
			t.Fatalf("content = %q, want %q", data, content)
		}
	})

	t.Run("commit then read back a sha512 blob", func(t *testing.T) {
		s := newStore(t)
		content := []byte("content addressed under sha512")
		want := sha512Digest(content)

		up, err := s.StartUpload(ctx)
		if err != nil {
			t.Fatalf("StartUpload: %v", err)
		}
		mustWrite(t, up, content)
		desc, err := up.Commit(ctx, want)
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if desc.Digest != want {
			t.Fatalf("Digest = %s, want %s", desc.Digest, want)
		}
		if data := readBlob(t, s, desc.Digest); !bytes.Equal(data, content) {
			t.Fatalf("content = %q, want %q", data, content)
		}

		// Walk must enumerate a sha512 blob alongside sha256 ones.
		found := false
		if err := s.Walk(ctx, func(info blob.Info) error {
			if info.Digest == want {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if !found {
			t.Errorf("Walk did not visit the sha512 blob %s", want)
		}
	})

	t.Run("stat and open unknown digest return ErrNotFound", func(t *testing.T) {
		s := newStore(t)
		unknown := blob.DigestBytes([]byte("nothing here"))
		if _, err := s.Stat(ctx, unknown); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Stat: got %v, want ErrNotFound", err)
		}
		if _, err := s.Open(ctx, unknown); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Open: got %v, want ErrNotFound", err)
		}
	})

	t.Run("upload resumes across sessions", func(t *testing.T) {
		s := newStore(t)
		content := []byte("resumable upload content")

		up, err := s.StartUpload(ctx)
		if err != nil {
			t.Fatalf("StartUpload: %v", err)
		}
		mustWrite(t, up, content[:10])
		id := up.ID()
		if err := up.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		resumed, err := s.ResumeUpload(ctx, id)
		if err != nil {
			t.Fatalf("ResumeUpload: %v", err)
		}
		if resumed.Size() != 10 {
			t.Fatalf("resumed Size = %d, want 10", resumed.Size())
		}
		mustWrite(t, resumed, content[10:])
		desc, err := resumed.Commit(ctx, blob.DigestBytes(content))
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if data := readBlob(t, s, desc.Digest); !bytes.Equal(data, content) {
			t.Fatalf("content = %q, want %q", data, content)
		}
	})

	t.Run("digest mismatch is rejected and stores nothing", func(t *testing.T) {
		s := newStore(t)
		content := []byte("real content")

		up, err := s.StartUpload(ctx)
		if err != nil {
			t.Fatalf("StartUpload: %v", err)
		}
		mustWrite(t, up, content)
		_, err = up.Commit(ctx, blob.DigestBytes([]byte("different content")))
		if !errors.Is(err, blob.ErrDigestMismatch) {
			t.Fatalf("Commit: got %v, want ErrDigestMismatch", err)
		}
		if _, err := s.Stat(ctx, blob.DigestBytes(content)); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("mismatched content must not be stored: %v", err)
		}
	})

	t.Run("committing identical content twice is idempotent", func(t *testing.T) {
		s := newStore(t)
		content := []byte("shared layer")
		a := putBlob(t, s, content)
		b := putBlob(t, s, content)
		if a != b {
			t.Fatalf("descriptors differ: %+v vs %+v", a, b)
		}
	})

	t.Run("delete removes the blob and is idempotent", func(t *testing.T) {
		s := newStore(t)
		desc := putBlob(t, s, []byte("to be deleted"))

		if err := s.Delete(ctx, desc.Digest); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Stat(ctx, desc.Digest); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("after Delete: got %v, want ErrNotFound", err)
		}
		if err := s.Delete(ctx, desc.Digest); err != nil {
			t.Errorf("deleting a missing blob must be a no-op: %v", err)
		}
	})

	t.Run("aborted upload cannot be resumed", func(t *testing.T) {
		s := newStore(t)
		up, err := s.StartUpload(ctx)
		if err != nil {
			t.Fatalf("StartUpload: %v", err)
		}
		mustWrite(t, up, []byte("partial"))
		id := up.ID()
		if err := up.Abort(ctx); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if _, err := s.ResumeUpload(ctx, id); !errors.Is(err, blob.ErrUploadNotFound) {
			t.Errorf("ResumeUpload after Abort: got %v, want ErrUploadNotFound", err)
		}
	})

	t.Run("walk visits every committed blob once", func(t *testing.T) {
		s := newStore(t)
		want := map[string]int64{}
		for _, c := range [][]byte{[]byte("blob one"), []byte("blob two"), []byte("third blob!")} {
			desc := putBlob(t, s, c)
			want[desc.Digest] = desc.Size
		}

		got := map[string]int64{}
		if err := s.Walk(ctx, func(info blob.Info) error {
			if _, seen := got[info.Digest]; seen {
				t.Errorf("blob %s visited twice", info.Digest)
			}
			got[info.Digest] = info.Size
			if info.ModTime.IsZero() {
				t.Errorf("blob %s has zero ModTime", info.Digest)
			}
			return nil
		}); err != nil {
			t.Fatalf("Walk: %v", err)
		}

		if len(got) != len(want) {
			t.Fatalf("walked %d blobs, want %d", len(got), len(want))
		}
		for digest, size := range want {
			if got[digest] != size {
				t.Errorf("blob %s size = %d, want %d", digest, got[digest], size)
			}
		}
	})

	t.Run("walk on an empty store visits nothing", func(t *testing.T) {
		s := newStore(t)
		count := 0
		if err := s.Walk(ctx, func(blob.Info) error { count++; return nil }); err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if count != 0 {
			t.Errorf("walked %d blobs on empty store, want 0", count)
		}
	})

	t.Run("malformed digests are rejected", func(t *testing.T) {
		s := newStore(t)
		for _, bad := range []string{"", "sha256:xyz", "md5:abc", "deadbeef"} {
			if _, err := s.Stat(ctx, bad); !errors.Is(err, blob.ErrInvalidDigest) {
				t.Errorf("Stat(%q): got %v, want ErrInvalidDigest", bad, err)
			}
		}
	})
}

func putBlob(t *testing.T, s blob.Store, content []byte) blob.Descriptor {
	t.Helper()
	ctx := context.Background()
	up, err := s.StartUpload(ctx)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	mustWrite(t, up, content)
	desc, err := up.Commit(ctx, blob.DigestBytes(content))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return desc
}

func mustWrite(t *testing.T, w io.Writer, p []byte) {
	t.Helper()
	if _, err := w.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func readBlob(t *testing.T, s blob.Store, digest string) []byte {
	t.Helper()
	rc, err := s.Open(context.Background(), digest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return data
}
