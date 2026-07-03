package cargo

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/repository"
)

// yank marks a version yanked (DELETE .../yank); unyank clears it (PUT .../unyank).
func (h *handler) yank(w http.ResponseWriter, r *http.Request) { h.setYank(w, r, true) }

func (h *handler) unyank(w http.ResponseWriter, r *http.Request) { h.setYank(w, r, false) }

func (h *handler) setYank(w http.ResponseWriter, r *http.Request, yanked bool) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot yank in a proxy repository")
		return
	}
	name := chi.URLParam(r, "crate")
	ver := chi.URLParam(r, "version")
	if err := h.store.setYanked(r.Context(), repo.ID, repo.ProjectID, name, ver, yanked, actorFrom(r)); err != nil {
		if errors.Is(err, ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "setting yanked", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
