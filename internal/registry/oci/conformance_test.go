package oci_test

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/platbor/platbor/internal/core/blob"
)

// These tests lock in behavior the OCI distribution-spec conformance suite
// exercises, so the guarantees survive without a live registry in CI.

func sha512Digest(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512:" + hex.EncodeToString(sum[:])
}

// TestManifestSubjectHeader asserts that pushing a manifest with a subject
// echoes the OCI-Subject response header, which is how a client learns the
// referrers link was recorded.
func TestManifestSubjectHeader(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"

	imgBody, imgDigest := h.buildImage(t, name)
	if put := h.putManifest(t, name, "v1.0", imgBody); put.Code != http.StatusCreated {
		t.Fatalf("push image: status = %d", put.Code)
	}

	refBody, refDigest := h.buildReferrer(t, name, imgDigest, len(imgBody), "application/vnd.example.signature")
	put := h.putManifest(t, name, refDigest, refBody)
	if put.Code != http.StatusCreated {
		t.Fatalf("push referrer: status = %d; body=%s", put.Code, put.Body.String())
	}
	if got := put.Header().Get("OCI-Subject"); got != imgDigest {
		t.Errorf("OCI-Subject = %q, want %q", got, imgDigest)
	}

	// A plain image with no subject must NOT carry the header.
	plain := h.putManifest(t, name, "v2.0", imgBody)
	if got := plain.Header().Get("OCI-Subject"); got != "" {
		t.Errorf("OCI-Subject on subjectless manifest = %q, want empty", got)
	}
}

// TestOutOfOrderChunkOnPutReturns416 covers a closing PUT that carries a chunk
// whose Content-Range does not continue from the current offset: the spec
// requires 416, not a later digest mismatch.
func TestOutOfOrderChunkOnPutReturns416(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"

	start := h.req(t, http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, "password")
	loc := start.Header().Get("Location")
	if put := h.req(t, http.MethodPatch, loc, []byte("first chunk"), "password"); put.Code != http.StatusAccepted {
		t.Fatalf("PATCH first chunk: status = %d", put.Code)
	}

	// The upload now holds 11 bytes; a PUT chunk that claims to start at offset 99
	// is out of order.
	req := httptest.NewRequest(http.MethodPut, loc+"?digest="+blob.DigestBytes([]byte("first chunkmore")), nil)
	req.SetBasicAuth("admin", "password")
	req.Header.Set("Content-Range", "99-110")
	req.Body = http.NoBody
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("out-of-order PUT: status = %d, want 416; body=%s", rr.Code, rr.Body.String())
	}
}

// TestManifestPushPullBySHA512Digest proves a manifest can be stored and
// retrieved under a sha512 digest, the algorithm the sha512 conformance data
// test pins.
func TestManifestPushPullBySHA512Digest(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"

	body, _ := h.buildImage(t, name)
	digest := sha512Digest(body)

	put := h.putManifest(t, name, digest, body)
	if put.Code != http.StatusCreated {
		t.Fatalf("PUT by sha512: status = %d, want 201; body=%s", put.Code, put.Body.String())
	}
	if got := put.Header().Get("Docker-Content-Digest"); got != digest {
		t.Errorf("Docker-Content-Digest = %q, want %q", got, digest)
	}

	get := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/"+digest, nil, "password")
	if get.Code != http.StatusOK {
		t.Fatalf("GET by sha512: status = %d, want 200", get.Code)
	}
	if got := get.Header().Get("Docker-Content-Digest"); got != digest {
		t.Errorf("GET digest header = %q, want %q", got, digest)
	}

	// A body that does not hash to the sha512 reference is rejected.
	if bad := h.putManifest(t, name, sha512Digest([]byte("other")), body); bad.Code != http.StatusBadRequest {
		t.Errorf("mismatched sha512 PUT: status = %d, want 400", bad.Code)
	}
}

// TestNonDistributableLayerAccepted covers a manifest whose layer lives outside
// the registry (carries urls): its blob is not required to be present.
func TestNonDistributableLayerAccepted(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"

	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDigest := h.pushBlob(t, name, config)
	// This layer's blob is never uploaded; only its external url is known.
	foreign := []byte("a non-distributable base layer that lives elsewhere")

	m := map[string]any{
		"schemaVersion": 2,
		"mediaType":     imageManifestType,
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest, "size": len(config)},
		"layers": []any{map[string]any{
			"mediaType": "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip",
			"digest":    blob.DigestBytes(foreign),
			"size":      len(foreign),
			"urls":      []string{"https://example.com/layers/base.tar.gz"},
		}},
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	if put := h.putManifest(t, name, "v1.0", body); put.Code != http.StatusCreated {
		t.Fatalf("PUT with non-distributable layer: status = %d, want 201; body=%s", put.Code, put.Body.String())
	}

	// A regular (non-url) layer that is missing must still be rejected.
	m["layers"] = []any{map[string]any{
		"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
		"digest":    blob.DigestBytes([]byte("missing regular layer")),
		"size":      21,
	}}
	missingBody, _ := json.Marshal(m)
	if put := h.putManifest(t, name, "v2.0", missingBody); put.Code != http.StatusBadRequest {
		t.Errorf("PUT with missing regular layer: status = %d, want 400", put.Code)
	}
}
