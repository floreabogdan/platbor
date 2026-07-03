package rubygems

import (
	"errors"
	"net/http"

	"github.com/platbor/platbor/internal/core/repository"
)

// yank marks a version yanked (DELETE /api/v1/gems/yank?gem_name=&version=). It
// is removed from the compact index (/versions and /info); the blob is reclaimed
// by a later GC sweep. --undo (unyank) is not part of the modern gem CLI, so only
// yank is supported.
func (h *handler) yank(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot yank in a proxy repository")
		return
	}
	name := r.URL.Query().Get("gem_name")
	version := r.URL.Query().Get("version")
	platform := r.URL.Query().Get("platform")
	if name == "" || version == "" {
		writeError(w, http.StatusBadRequest, "gem_name and version are required")
		return
	}
	number := version
	if platform != "" && platform != "ruby" {
		number = version + "-" + platform
	}
	if err := h.store.setYanked(r.Context(), repo.ID, repo.ProjectID, name, number, true, actorFrom(r)); err != nil {
		if errors.Is(err, ErrGemNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "yanking gem", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Successfully yanked gem: " + name + " (" + number + ")\n"))
}
