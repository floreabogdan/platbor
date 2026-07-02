package blob_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/platbor/platbor/internal/core/blob"
)

func newFS(t *testing.T) blob.Store {
	t.Helper()
	s, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return s
}

// TestFSStoreContract runs the driver-agnostic suite against the fs driver.
func TestFSStoreContract(t *testing.T) {
	runStoreContract(t, newFS)
}

// TestFSLayout pins the on-disk content-addressable layout, which other tools
// (backup, GC) and operators rely on.
func TestFSLayout(t *testing.T) {
	root := t.TempDir()
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	content := []byte("layout check")
	desc := putBlob(t, s, content)

	// digest is "sha256:<hex>"; expect blobs/sha256/<ab>/<hex>.
	hexPart := desc.Digest[len("sha256:"):]
	want := filepath.Join(root, "blobs", "sha256", hexPart[:2], hexPart)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected blob at %s: %v", want, err)
	}
}

// TestFSUploadIsTemporary verifies an in-progress upload lives outside the CAS
// and does not leak into it after commit.
func TestFSUploadIsTemporary(t *testing.T) {
	root := t.TempDir()
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	ctx := context.Background()

	up, err := s.StartUpload(ctx)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	mustWrite(t, up, []byte("temp data"))
	if err := up.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The temp file exists under uploads/ while the session is open.
	if _, err := os.Stat(filepath.Join(root, "uploads", up.ID())); err != nil {
		t.Fatalf("expected upload temp file: %v", err)
	}

	resumed, err := s.ResumeUpload(ctx, up.ID())
	if err != nil {
		t.Fatalf("ResumeUpload: %v", err)
	}
	if _, err := resumed.Commit(ctx, blob.DigestBytes([]byte("temp data"))); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// After commit the temp file is gone.
	if _, err := os.Stat(filepath.Join(root, "uploads", up.ID())); !os.IsNotExist(err) {
		t.Errorf("upload temp file should be removed after commit, stat err = %v", err)
	}
}
