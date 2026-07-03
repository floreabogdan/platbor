package npm

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// getPackument answers `npm install`'s metadata request. For a proxy project it
// fetches fresh from the upstream (falling back to cache when offline); for a
// local project it rebuilds the packument from stored versions and dist-tags.
func (h *handler) getPackument(w http.ResponseWriter, r *http.Request, project, pkg string) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}
	up, isProxy, err := h.proxyUpstreamFor(r.Context(), projectID)
	if err != nil {
		h.internalError(w, "checking proxy", err)
		return
	}
	if isProxy {
		h.proxyPackument(w, r, up, projectID, project, pkg)
		return
	}

	versions, distTags, err := h.store.packument(r.Context(), projectID, pkg)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "package not found: "+pkg)
			return
		}
		h.internalError(w, "reading packument", err)
		return
	}
	h.writePackument(w, r, project, pkg, versions, distTags)
}

// writePackument assembles and writes a package document from stored versions,
// stamping each version's dist with this registry's tarball URL and the
// authoritative digests computed at publish time.
func (h *handler) writePackument(w http.ResponseWriter, r *http.Request, project, pkg string, versions []storedVersion, distTags map[string]string) {
	base := registryBase(r, project)
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

// getTarball serves a version's tarball. For a proxy project it fills the cache
// from the upstream on a miss; for a local project it streams from the store.
func (h *handler) getTarball(w http.ResponseWriter, r *http.Request, project, pkg, filename string) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}
	up, isProxy, err := h.proxyUpstreamFor(r.Context(), projectID)
	if err != nil {
		h.internalError(w, "checking proxy", err)
		return
	}
	if isProxy {
		h.proxyTarball(w, r, up, projectID, pkg, filename)
		return
	}

	version, ok := versionFromFilename(pkg, filename)
	if !ok {
		writeError(w, h.log, http.StatusNotFound, "not found")
		return
	}
	digest, size, err := h.store.tarball(r.Context(), projectID, pkg, version)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving tarball", err)
		return
	}
	h.streamBlob(w, r, digest, size)
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

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
