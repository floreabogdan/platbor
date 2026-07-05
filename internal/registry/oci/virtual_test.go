package oci_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
)

// TestVirtualRepositoryResolvesReadsAndDeniesWrites pushes an image into a local
// member repository, aggregates it behind a virtual repository, and pulls it back
// through the aggregate — proving read resolution across members — then confirms
// the virtual repository rejects writes and unions member tags.
func TestVirtualRepositoryResolvesReadsAndDeniesWrites(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Push an image into a local member repository.
	imgBody, imgDigest := h.buildImage(t, "library/local-a/app")
	if put := h.putManifest(t, "library/local-a/app", "v1", imgBody); put.Code != http.StatusCreated {
		t.Fatalf("push image: status = %d; body=%s", put.Code, put.Body.String())
	}

	// Create a virtual repository aggregating that member.
	proj, err := project.NewService(h.db).GetByKey(ctx, "library")
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	if _, err := repository.NewService(h.db).Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "group", Name: "group", Format: repository.FormatOCI,
		Mode: repository.ModeVirtual, MemberKeys: []string{"local-a"}, Actor: "admin",
	}); err != nil {
		t.Fatalf("create virtual repo: %v", err)
	}

	// Pull the image through the virtual repository, by tag and by digest.
	for _, ref := range []string{"v1", imgDigest} {
		rr := h.req(t, http.MethodGet, "/v2/library/group/app/manifests/"+ref, nil, "password")
		if rr.Code != http.StatusOK {
			t.Fatalf("GET manifest via virtual repo (%s): status = %d", ref, rr.Code)
		}
		if got := rr.Header().Get("Docker-Content-Digest"); got != imgDigest {
			t.Errorf("digest via virtual repo (%s) = %q, want %q", ref, got, imgDigest)
		}
	}

	// A missing reference is a clean 404 (no member has it).
	miss := h.req(t, http.MethodGet, "/v2/library/group/app/manifests/nope", nil, "password")
	if miss.Code != http.StatusNotFound {
		t.Errorf("GET missing tag: status = %d, want 404", miss.Code)
	}

	// Tag listing unions the members.
	tags := h.req(t, http.MethodGet, "/v2/library/group/app/tags/list", nil, "password")
	if tags.Code != http.StatusOK {
		t.Fatalf("GET tags/list: status = %d", tags.Code)
	}
	var tl struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(tags.Body.Bytes(), &tl); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	if len(tl.Tags) != 1 || tl.Tags[0] != "v1" {
		t.Errorf("virtual tags = %v, want [v1]", tl.Tags)
	}

	// Writes to a virtual repository are rejected.
	put := h.putManifest(t, "library/group/app", "v2", imgBody)
	if put.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT to virtual repo: status = %d, want 405", put.Code)
	}
}
