package oci

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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
	if limit > 0 {
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
