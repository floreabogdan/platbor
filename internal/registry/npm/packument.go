package npm

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/platbor/platbor/internal/core/blob"
)

// getPackument answers `npm install`'s metadata request: the package document
// with every version and the current dist-tags. Each version's dist.tarball is
// rewritten to point at this server, and dist.shasum/integrity are stamped with
// the authoritative digests computed at publish time.
func (h *handler) getPackument(w http.ResponseWriter, r *http.Request, project, repo, pkg string) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}

	versions, distTags, err := h.store.packument(r.Context(), projectID, repo, pkg)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "package not found: "+pkg)
			return
		}
		h.internalError(w, "reading packument", err)
		return
	}

	base := registryBase(r, project, repo)
	versionMap := make(map[string]json.RawMessage, len(versions))
	for _, v := range versions {
		patched, err := patchVersion(v, base, pkg)
		if err != nil {
			h.internalError(w, "assembling version", err)
			return
		}
		versionMap[v.Version] = patched
	}

	doc := map[string]any{
		"_id":       pkg,
		"name":      pkg,
		"dist-tags": distTags,
		"versions":  versionMap,
	}
	writeJSON(w, h.log, http.StatusOK, doc)
}

// patchVersion rewrites the stored version document's dist block so downloads
// resolve to this registry with authoritative digests.
func patchVersion(v storedVersion, base, pkg string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v.Manifest, &obj); err != nil {
		return nil, err
	}

	var dist map[string]json.RawMessage
	if raw, ok := obj["dist"]; ok {
		if err := json.Unmarshal(raw, &dist); err != nil {
			dist = nil
		}
	}
	if dist == nil {
		dist = map[string]json.RawMessage{}
	}
	dist["tarball"] = jsonString(tarballURL(base, pkg, v.Version))
	dist["shasum"] = jsonString(v.Shasum)
	if v.Integrity != "" {
		dist["integrity"] = jsonString(v.Integrity)
	}

	distBytes, err := json.Marshal(dist)
	if err != nil {
		return nil, err
	}
	obj["dist"] = distBytes
	return json.Marshal(obj)
}

// getTarball streams a published version's tarball from the blob store.
func (h *handler) getTarball(w http.ResponseWriter, r *http.Request, project, repo, pkg, filename string) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}
	version, ok := versionFromFilename(pkg, filename)
	if !ok {
		writeError(w, h.log, http.StatusNotFound, "not found")
		return
	}

	digest, size, err := h.store.tarball(r.Context(), projectID, repo, pkg, version)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving tarball", err)
		return
	}

	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening tarball", err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming tarball", "error", err.Error())
	}
}

// tarballURL builds the canonical download URL for a version:
// <base>/<pkg>/-/<basename>-<version>.tgz.
func tarballURL(base, pkg, version string) string {
	basename := pkg
	if i := lastSlash(basename); i >= 0 {
		basename = basename[i+1:]
	}
	return base + "/" + pkg + "/-/" + basename + "-" + version + ".tgz"
}

// jsonString marshals s as a JSON string; encoding a plain string never fails.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
