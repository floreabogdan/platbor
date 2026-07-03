package npm

import (
	"encoding/json"
	"errors"
	"net/http"
)

// serveDistTags handles the `npm dist-tag` family under
// /-/package/<pkg>/dist-tags[/<tag>]: list (GET), set (PUT), and remove (DELETE).
func (h *handler) serveDistTags(w http.ResponseWriter, r *http.Request, project string, op npmOp) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listDistTags(w, r, projectID, op.pkg)
	case http.MethodPut:
		h.setDistTag(w, r, projectID, op.pkg, op.ref)
	case http.MethodDelete:
		h.deleteDistTag(w, r, projectID, op.pkg, op.ref)
	default:
		writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *handler) listDistTags(w http.ResponseWriter, r *http.Request, projectID, pkg string) {
	tags, err := h.store.distTags(r.Context(), projectID, pkg)
	if err != nil {
		h.internalError(w, "listing dist-tags", err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, tags)
}

func (h *handler) setDistTag(w http.ResponseWriter, r *http.Request, projectID, pkg, tag string) {
	if tag == "" {
		writeError(w, h.log, http.StatusBadRequest, "missing dist-tag")
		return
	}
	// The body is a JSON string: the version the tag should point at.
	var version string
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&version); err != nil || version == "" {
		writeError(w, h.log, http.StatusBadRequest, "invalid version")
		return
	}

	if err := h.store.setDistTag(r.Context(), projectID, pkg, tag, version, actorFrom(r)); err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "package not found: "+pkg)
			return
		}
		h.internalError(w, "setting dist-tag", err)
		return
	}
	writeJSON(w, h.log, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *handler) deleteDistTag(w http.ResponseWriter, r *http.Request, projectID, pkg, tag string) {
	if tag == "" {
		writeError(w, h.log, http.StatusBadRequest, "missing dist-tag")
		return
	}
	if tag == "latest" {
		writeError(w, h.log, http.StatusBadRequest, "cannot remove the latest dist-tag")
		return
	}

	if err := h.store.deleteDistTag(r.Context(), projectID, pkg, tag, actorFrom(r)); err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "deleting dist-tag", err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, map[string]bool{"ok": true})
}
