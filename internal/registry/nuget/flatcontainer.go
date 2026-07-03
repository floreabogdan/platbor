package nuget

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
)

// flatContainer serves the PackageBaseAddress resource used by restore:
//
//	<id-lower>/index.json                          -> the version list
//	<id-lower>/<version>/<id-lower>.<version>.nupkg -> the package download
func (h *handler) flatContainer(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	rest := strings.Trim(chi.URLParam(r, "*"), "/")
	parts := strings.Split(rest, "/")

	switch {
	case len(parts) == 2 && parts[1] == "index.json":
		h.flatVersions(w, r, repo.ID, strings.ToLower(parts[0]))
	case len(parts) == 3 && strings.HasSuffix(strings.ToLower(parts[2]), ".nupkg"):
		h.flatDownload(w, r, repo.ID, strings.ToLower(parts[0]), strings.ToLower(parts[1]))
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// flatVersions returns the normalized version list for a package.
func (h *handler) flatVersions(w http.ResponseWriter, r *http.Request, repositoryID, idLower string) {
	versions, err := h.store.versions(r.Context(), repositoryID, idLower)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, http.StatusNotFound, "package not found")
			return
		}
		h.internalError(w, "listing versions", err)
		return
	}
	list := make([]string, 0, len(versions))
	for _, v := range versions {
		list = append(list, v.VersionLower)
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": list})
}

// flatDownload streams a version's .nupkg from the blob store.
func (h *handler) flatDownload(w http.ResponseWriter, r *http.Request, repositoryID, idLower, versionLower string) {
	digest, size, err := h.store.nupkg(r.Context(), repositoryID, idLower, versionLower)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving package", err)
		return
	}
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening package", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming nupkg", "error", err.Error())
	}
}
