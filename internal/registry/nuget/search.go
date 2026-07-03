package nuget

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// search serves the SearchQueryService: a minimal search over package ids in the
// feed. Supports ?q=, ?take=, and returns each package's versions.
func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	query := r.URL.Query().Get("q")
	take := 20
	if t := r.URL.Query().Get("take"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 && n <= 1000 {
			take = n
		}
	}

	hits, err := h.store.search(r.Context(), repo.ID, query, take)
	if err != nil {
		h.internalError(w, "searching", err)
		return
	}

	base := baseURL(r, chi.URLParam(r, "project"), chi.URLParam(r, "repo"))
	data := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		versions, err := h.store.versions(r.Context(), repo.ID, hit.IDLower)
		if err != nil {
			continue
		}
		versionList := make([]map[string]any, 0, len(versions))
		for _, v := range versions {
			versionList = append(versionList, map[string]any{
				"version":   v.Version,
				"downloads": 0,
				"@id":       base + "/v3/registrations/" + hit.IDLower + "/index.json#" + v.Version,
			})
		}
		latest := versions[len(versions)-1]
		data = append(data, map[string]any{
			"@type":          "Package",
			"id":             hit.ID,
			"version":        latest.Version,
			"versions":       versionList,
			"totalDownloads": 0,
			"registration":   base + "/v3/registrations/" + hit.IDLower + "/index.json",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"@context":  map[string]any{"@base": base + "/v3/registrations/"},
		"totalHits": len(data),
		"data":      data,
	})
}
