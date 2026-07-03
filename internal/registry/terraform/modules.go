package terraform

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
)

// versions implements GET /v1/modules/<namespace>/<name>/<provider>/versions:
// the Terraform module version listing.
func (h *handler) versions(w http.ResponseWriter, r *http.Request) {
	moduleID, _, ok := h.resolveRead(w, r)
	if !ok {
		return
	}
	vers, err := h.store.listVersions(r.Context(), moduleID)
	if err != nil {
		h.internalError(w, "listing versions", err)
		return
	}
	if len(vers) == 0 {
		writeError(w, http.StatusNotFound, "module not found")
		return
	}
	// The response shape is {"modules":[{"versions":[{"version":"1.0.0"},...]}]}.
	items := make([]map[string]string, 0, len(vers))
	for _, v := range vers {
		items = append(items, map[string]string{"version": v})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"modules": []any{map[string]any{"versions": items}},
	})
}

// download implements GET /v1/modules/<ns>/<name>/<provider>/<version>/download:
// a 204 whose X-Terraform-Get header points at our archive endpoint (forced to
// tar.gz extraction).
func (h *handler) download(w http.ResponseWriter, r *http.Request) {
	moduleID, _, ok := h.resolveRead(w, r)
	if !ok {
		return
	}
	version := chi.URLParam(r, "version")
	if _, _, err := h.store.getVersion(r.Context(), moduleID, version); err != nil {
		if errors.Is(err, ErrModuleNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving version", err)
		return
	}
	get := archiveURL(r)
	w.Header().Set("X-Terraform-Get", get)
	w.WriteHeader(http.StatusNoContent)
}

// archive implements GET /v1/modules/<ns>/<name>/<provider>/<version>/archive:
// the module archive bytes (a tar.gz).
func (h *handler) archive(w http.ResponseWriter, r *http.Request) {
	moduleID, _, ok := h.resolveRead(w, r)
	if !ok {
		return
	}
	version := chi.URLParam(r, "version")
	digest, size, err := h.store.getVersion(r.Context(), moduleID, version)
	if err != nil {
		if errors.Is(err, ErrModuleNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving version", err)
		return
	}
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening archive", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	name := chi.URLParam(r, "name")
	provider := chi.URLParam(r, "provider")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"-"+provider+"-"+version+".tar.gz\"")
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming module archive", "error", err.Error())
	}
}

// resolveRead resolves the read-protocol path params ({namespace}/{name}/
// {provider}) to a module, authorizing read on the namespace's project. It writes
// the error response itself on any problem.
func (h *handler) resolveRead(w http.ResponseWriter, r *http.Request) (moduleID, repoID string, ok bool) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	provider := chi.URLParam(r, "provider")

	projectID, err := h.store.projectID(r.Context(), namespace)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "namespace not found: "+namespace)
			return "", "", false
		}
		h.internalError(w, "resolving namespace", err)
		return "", "", false
	}
	if !h.authorize(w, r, projectID, false) {
		return "", "", false
	}
	moduleID, repoID, err = h.store.resolveModule(r.Context(), projectID, name, provider)
	if err != nil {
		if errors.Is(err, ErrModuleNotFound) {
			writeError(w, http.StatusNotFound, "module not found")
			return "", "", false
		}
		h.internalError(w, "resolving module", err)
		return "", "", false
	}
	return moduleID, repoID, true
}

// archiveURL builds the absolute URL of the archive endpoint for the current
// download request, with the archive type forced so terraform's go-getter
// extracts it.
func archiveURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	provider := chi.URLParam(r, "provider")
	version := chi.URLParam(r, "version")
	return scheme + "://" + r.Host + "/terraform/v1/modules/" + ns + "/" + name + "/" + provider + "/" + version + "/archive?archive=tar.gz"
}
