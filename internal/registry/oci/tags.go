package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/platbor/platbor/internal/core/repository"
)

// Tag-listing page bounds. `n` requests a page size; we cap it so a single
// request can never ask the store for an unbounded scan.
const (
	defaultTagPage = 100
	maxTagPage     = 1000
)

// tagList is the GET /v2/<name>/tags/list response body.
type tagList struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// serveTags lists a repository's tags, lexically ordered, with the spec's
// `n`/`last` keyset pagination and a Link header when more pages remain.
func (h *handler) serveTags(w http.ResponseWriter, r *http.Request, p parsedPath) {
	if r.Method != http.MethodGet {
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
		return
	}
	repo, image, ok := h.resolveRepo(w, r, p.name, false)
	if !ok {
		return
	}

	limit := defaultTagPage
	if n := r.URL.Query().Get("n"); n != "" {
		v, err := strconv.Atoi(n)
		if err != nil || v < 0 {
			writeError(w, h.log, http.StatusBadRequest, codeManifestInvalid, "invalid n parameter")
			return
		}
		limit = v
	}
	if limit > maxTagPage {
		limit = maxTagPage
	}
	last := r.URL.Query().Get("last")

	tags := []string{}
	var next string
	switch {
	case repo.Mode == repository.ModeVirtual:
		var err error
		tags, next, err = h.virtualTags(r.Context(), repo, image, last, limit)
		if err != nil {
			h.internalError(w, "listing virtual tags", err)
			return
		}
	case limit > 0:
		// Fetch one extra row to detect whether a further page exists.
		page, err := h.manifests.listTags(r.Context(), repo.ID, image, last, limit+1)
		if err != nil {
			h.internalError(w, "listing tags", err)
			return
		}
		if len(page) > limit {
			next = page[limit-1]
			page = page[:limit]
		}
		tags = page
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if next != "" {
		w.Header().Set("Link", fmt.Sprintf(`</v2/%s/tags/list?last=%s&n=%d>; rel="next"`, p.name, url.QueryEscape(next), limit))
	}
	if err := json.NewEncoder(w).Encode(tagList{Name: p.name, Tags: tags}); err != nil {
		h.log.Error("encoding tag list", slog.String("error", err.Error()))
	}
}

// virtualTags returns one page of a virtual repository's tag list: the union of
// its members' tags for the image, lexically ordered, applying the spec's
// `last`/`n` keyset pagination in memory. Each member is scanned up to maxTagPage
// tags; a member with more is truncated (a group over enormous member tag sets is
// an edge case, and the per-member cap bounds the work).
func (h *handler) virtualTags(ctx context.Context, repo repository.Repository, image, last string, limit int) ([]string, string, error) {
	if limit <= 0 {
		return []string{}, "", nil
	}
	members, err := h.repos.Members(ctx, repo.ID)
	if err != nil {
		return nil, "", err
	}
	set := make(map[string]struct{})
	for _, member := range members {
		page, err := h.manifests.listTags(ctx, member.ID, image, "", maxTagPage)
		if err != nil {
			return nil, "", err
		}
		for _, t := range page {
			set[t] = struct{}{}
		}
	}
	all := make([]string, 0, len(set))
	for t := range set {
		if t > last { // keyset: strictly after the cursor
			all = append(all, t)
		}
	}
	sort.Strings(all)

	next := ""
	if len(all) > limit {
		next = all[limit-1]
		all = all[:limit]
	}
	return all, next, nil
}
