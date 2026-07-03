package npm

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/platbor/platbor/internal/core/blob"
)

// maxPublishBody caps a publish payload. Tarballs are base64-inlined in the
// JSON body, so this bounds the largest package we accept in one publish.
const maxPublishBody = 256 << 20 // 256 MiB

// publishRequest is the document `npm publish` PUTs to /<package>. It carries
// the new version metadata under "versions", the tags to move under
// "dist-tags", and the tarball bytes (base64) under "_attachments".
type publishRequest struct {
	Name        string                     `json:"name"`
	DistTags    map[string]string          `json:"dist-tags"`
	Versions    map[string]json.RawMessage `json:"versions"`
	Attachments map[string]attachment      `json:"_attachments"`
}

type attachment struct {
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64-encoded tarball
	Length      int64  `json:"length"`
}

// versionDist is the subset of a version's metadata we read: the integrity and
// shasum the client computed, which we verify against the tarball we receive.
type versionDist struct {
	Dist struct {
		Shasum    string `json:"shasum"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// publish handles `npm publish`: verify each version's tarball, store it in the
// content-addressable blob store, then persist the package, versions, and
// dist-tags atomically. Proxy projects are read-only and reject publishes.
func (h *handler) publish(w http.ResponseWriter, r *http.Request, project, pkg string) {
	projectID, ok := h.resolveProject(w, r, project)
	if !ok {
		return
	}
	if proxy, err := h.store.isProxy(r.Context(), projectID); err != nil {
		h.internalError(w, "checking proxy", err)
		return
	} else if proxy {
		writeError(w, h.log, http.StatusForbidden, "cannot publish to a proxy project")
		return
	}

	var req publishRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPublishBody)).Decode(&req); err != nil {
		writeError(w, h.log, http.StatusBadRequest, "invalid publish payload")
		return
	}
	if req.Name != pkg {
		writeError(w, h.log, http.StatusBadRequest, "package name mismatch")
		return
	}
	if len(req.Versions) == 0 {
		writeError(w, h.log, http.StatusBadRequest, "no versions in publish")
		return
	}

	versions := make([]versionInput, 0, len(req.Versions))
	for version, raw := range req.Versions {
		vi, ok := h.prepareVersion(w, r, pkg, version, raw, req.Attachments)
		if !ok {
			return
		}
		versions = append(versions, vi)
	}

	in := publishInput{
		ProjectID: projectID,
		Name:      pkg,
		Versions:  versions,
		DistTags:  req.DistTags,
		Actor:     actorFrom(r),
	}
	if err := h.store.publish(r.Context(), in); err != nil {
		if errors.Is(err, ErrVersionExists) {
			writeError(w, h.log, http.StatusConflict, "cannot publish over an existing version")
			return
		}
		h.internalError(w, "publishing package", err)
		return
	}

	writeJSON(w, h.log, http.StatusCreated, map[string]any{"ok": true, "success": true})
}

// prepareVersion decodes and verifies one version's tarball attachment, commits
// it to the blob store, and returns the version record to persist. It writes the
// error response itself and returns ok=false on any problem.
func (h *handler) prepareVersion(w http.ResponseWriter, r *http.Request, pkg, version string, raw json.RawMessage, attachments map[string]attachment) (versionInput, bool) {
	// npm keys _attachments by the full package name, including any @scope/
	// prefix: "@acme/widgets-1.2.3.tgz" (the download URL, by contrast, uses the
	// unscoped basename: .../@acme/widgets/-/widgets-1.2.3.tgz).
	filename := pkg + "-" + version + ".tgz"

	att, ok := attachments[filename]
	if !ok {
		writeError(w, h.log, http.StatusBadRequest, "missing tarball attachment for "+filename)
		return versionInput{}, false
	}
	tarball, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		writeError(w, h.log, http.StatusBadRequest, "invalid tarball encoding")
		return versionInput{}, false
	}

	shasum := sha1Hex(tarball)
	integrity := sha512SRI(tarball)

	// Verify against the digests the client computed, when present, so a
	// corrupted or tampered upload is rejected rather than stored.
	var dist versionDist
	if err := json.Unmarshal(raw, &dist); err == nil {
		if dist.Dist.Shasum != "" && dist.Dist.Shasum != shasum {
			writeError(w, h.log, http.StatusBadRequest, "tarball shasum mismatch")
			return versionInput{}, false
		}
		if dist.Dist.Integrity != "" && dist.Dist.Integrity != integrity {
			writeError(w, h.log, http.StatusBadRequest, "tarball integrity mismatch")
			return versionInput{}, false
		}
	}

	digest, size, err := h.storeTarball(r, tarball)
	if err != nil {
		h.internalError(w, "storing tarball", err)
		return versionInput{}, false
	}

	return versionInput{
		Version:       version,
		Manifest:      raw,
		TarballDigest: digest,
		TarballSize:   size,
		Shasum:        shasum,
		Integrity:     integrity,
	}, true
}

// storeTarball commits tarball bytes to the content-addressable blob store,
// returning its sha256 digest and size. Re-publishing identical content dedups.
func (h *handler) storeTarball(r *http.Request, tarball []byte) (digest string, size int64, err error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, fmt.Errorf("starting upload: %w", err)
	}
	if _, err := up.Write(tarball); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, fmt.Errorf("writing tarball: %w", err)
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(tarball))
	if err != nil {
		return "", 0, fmt.Errorf("committing tarball: %w", err)
	}
	return desc.Digest, desc.Size, nil
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// sha512SRI returns npm's integrity string: "sha512-" + base64(sha512(data)).
func sha512SRI(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
