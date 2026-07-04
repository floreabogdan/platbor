package blob_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/platbor/platbor/internal/core/blob"
)

// s3PrefixCounter gives each store instance an isolated key prefix so the
// contract subtests (which each build a fresh store over the same bucket) never
// see one another's blobs during Walk.
var s3PrefixCounter atomic.Int64

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// newS3 builds an S3-backed store against the endpoint in
// PLATBOR_TEST_S3_ENDPOINT (host:port), skipping when it is unset. Point it at a
// MinIO instance to run the same contract suite the fs driver passes:
//
//	PLATBOR_TEST_S3_ENDPOINT=localhost:9000 go test ./internal/core/blob/
func newS3(t *testing.T) blob.Store {
	t.Helper()
	endpoint := os.Getenv("PLATBOR_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PLATBOR_TEST_S3_ENDPOINT (host:port) to run the s3 contract suite")
	}
	s, err := blob.NewS3(context.Background(), blob.S3Options{
		Endpoint:   endpoint,
		Bucket:     envOr("PLATBOR_TEST_S3_BUCKET", "platbor-test"),
		AccessKey:  envOr("PLATBOR_TEST_S3_ACCESS_KEY", "minioadmin"),
		SecretKey:  envOr("PLATBOR_TEST_S3_SECRET_KEY", "minioadmin"),
		UseSSL:     false,
		Prefix:     fmt.Sprintf("contract/%d", s3PrefixCounter.Add(1)),
		StagingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return s
}

// TestS3StoreContract runs the driver-agnostic suite against the s3 driver — the
// Liskov enforcer: the s3 store must behave exactly like the fs store.
func TestS3StoreContract(t *testing.T) {
	runStoreContract(t, newS3)
}
