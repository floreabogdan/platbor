package cargo

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/repository"
)

// index serves a crate's sparse index file: newline-delimited JSON, one line per
// version. For a local repo the lines are read from the store; for a proxy the
// upstream index is fetched fresh, each version recorded for later caching, and
// the lines served back (cargo downloads via our config.json dl).
func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	path := chi.URLParam(r, "*")
	name, ok := crateFromIndexPath(path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if repo.Mode == repository.ModeProxy {
		h.proxyIndex(w, r, repo, name)
		return
	}

	lines, err := h.store.indexLines(r.Context(), repo.ID, normalizeName(name))
	if err != nil {
		h.internalError(w, "listing index", err)
		return
	}
	if len(lines) == 0 {
		writeError(w, http.StatusNotFound, "crate not found")
		return
	}
	writeIndex(w, lines)
}

// writeIndex writes newline-delimited index lines.
func writeIndex(w http.ResponseWriter, lines []string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
}
