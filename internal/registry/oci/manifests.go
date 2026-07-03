package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/platbor/platbor/internal/core/blob"
)

// maxManifestBytes caps a manifest document. Manifests are small descriptors
// (config + layer list), never the layers themselves; this rejects anything
// pathological long before it reaches storage.
const maxManifestBytes = 4 << 20 // 4 MiB

// Tag/index media types we recognize. A push must declare one of these (via the
// Content-Type header or the document's own mediaType field) so GET can echo it
// back verbatim.
var (
	imageManifestTypes = map[string]bool{
		"application/vnd.oci.image.manifest.v1+json":           true,
		"application/vnd.docker.distribution.manifest.v2+json": true,
	}
	imageIndexTypes = map[string]bool{
		"application/vnd.oci.image.index.v1+json":                   true,
		"application/vnd.docker.distribution.manifest.list.v2+json": true,
	}
)

// descriptor is the subset of an OCI descriptor we read: the digest and size of
// a referenced blob (config/layer) or child manifest, plus the platform an
// index entry targets.
type descriptor struct {
	MediaType string    `json:"mediaType"`
	Digest    string    `json:"digest"`
	Size      int64     `json:"size"`
	Platform  *platform `json:"platform,omitempty"`
	URLs      []string  `json:"urls,omitempty"`
}

// platform is an index entry's target OS/architecture.
type platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

// platformString renders "os/arch" (with an optional "/variant"), or "" when the
// descriptor carries no platform.
func (d descriptor) platformString() string {
	if d.Platform == nil || d.Platform.OS == "" {
		return ""
	}
	s := d.Platform.OS + "/" + d.Platform.Architecture
	if d.Platform.Variant != "" {
		s += "/" + d.Platform.Variant
	}
	return s
}

// manifestDoc is the union of the manifest shapes we parse: an image manifest
// (config + layers, which are blobs) or an index (manifests, which are other
// manifests), plus the fields the referrers API denormalizes — subject,
// artifactType, and annotations.
type manifestDoc struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType"`
	Config       *descriptor       `json:"config"`
	Layers       []descriptor      `json:"layers"`
	Manifests    []descriptor      `json:"manifests"`
	Subject      *descriptor       `json:"subject"`
	Annotations  map[string]string `json:"annotations"`
}

// subjectDigest is the digest of the manifest this one refers to, or "" if it
// stands alone.
func (d manifestDoc) subjectDigest() string {
	if d.Subject == nil {
		return ""
	}
	return d.Subject.Digest
}

// effectiveArtifactType is the manifest's artifactType, falling back to the
// config media type for image manifests (per the distribution spec).
func (d manifestDoc) effectiveArtifactType() string {
	if d.ArtifactType != "" {
		return d.ArtifactType
	}
	if d.Config != nil {
		return d.Config.MediaType
	}
	return ""
}

// manifestError is a client-facing rejection with its spec error code and HTTP
// status, distinguished from internal (500) errors.
type manifestError struct {
	status int
	code   string
	msg    string
}

func (e *manifestError) Error() string { return e.msg }

// serveManifest dispatches /v2/<name>/manifests/<ref> after resolving the
// project the repository belongs to.
func (h *handler) serveManifest(w http.ResponseWriter, r *http.Request, p parsedPath) {
	projectID, repo, ok := h.resolveName(w, r, p.name)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.putManifest(w, r, projectID, repo, p)
	case http.MethodGet:
		h.getManifest(w, r, projectID, repo, p, true)
	case http.MethodHead:
		h.getManifest(w, r, projectID, repo, p, false)
	case http.MethodDelete:
		h.deleteManifest(w, r, projectID, repo, p)
	default:
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
	}
}

// putManifest stores an uploaded manifest, verifying its digest and that every
// blob (or child manifest) it references already exists.
func (h *handler) putManifest(w http.ResponseWriter, r *http.Request, projectID, repo string, p parsedPath) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxManifestBytes+1))
	if err != nil {
		h.internalError(w, "reading manifest", err)
		return
	}
	if len(body) > maxManifestBytes {
		writeError(w, h.log, http.StatusRequestEntityTooLarge, codeManifestInvalid, "manifest exceeds size limit")
		return
	}

	var doc manifestDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		writeError(w, h.log, http.StatusBadRequest, codeManifestInvalid, "manifest is not valid JSON")
		return
	}

	mediaType := manifestMediaType(r.Header.Get("Content-Type"), doc.MediaType)
	if mediaType == "" {
		writeError(w, h.log, http.StatusBadRequest, codeManifestInvalid, "unknown or missing manifest media type")
		return
	}

	// sha256 is the canonical key for a manifest pushed by tag. A push by digest
	// may instead pin sha512; honor that algorithm so the manifest is stored and
	// retrievable under exactly the digest the client used.
	digest := blob.DigestBytes(body)

	// The reference is either a digest (which must match the content) or a tag.
	var tag string
	if isDigestRef(p.ref) {
		if err := blob.ValidateDigest(p.ref); err != nil {
			writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "invalid digest")
			return
		}
		if !blob.MatchesDigest(p.ref, body) {
			writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "manifest content does not match digest")
			return
		}
		digest = p.ref
	} else {
		if !validTag(p.ref) {
			writeError(w, h.log, http.StatusBadRequest, codeManifestInvalid, "invalid tag")
			return
		}
		tag = p.ref
	}

	if err := h.validateReferences(r.Context(), projectID, repo, mediaType, doc); err != nil {
		var me *manifestError
		if errors.As(err, &me) {
			writeError(w, h.log, me.status, me.code, me.msg)
			return
		}
		h.internalError(w, "validating manifest references", err)
		return
	}

	if err := h.manifests.putManifest(r.Context(), manifestWrite{
		ProjectID:    projectID,
		Repository:   repo,
		Digest:       digest,
		MediaType:    mediaType,
		Payload:      body,
		Size:         int64(len(body)),
		Tag:          tag,
		Subject:      doc.subjectDigest(),
		ArtifactType: doc.effectiveArtifactType(),
		Actor:        usernameFromContext(r.Context()),
	}); err != nil {
		h.internalError(w, "storing manifest", err)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Location", "/v2/"+p.name+"/manifests/"+digest)
	// A manifest that declares a subject participates in the referrers API; the
	// spec requires echoing the subject digest so the client knows the link was
	// recorded and it need not fall back to the tag scheme.
	if subject := doc.subjectDigest(); subject != "" {
		w.Header().Set("OCI-Subject", subject)
	}
	w.WriteHeader(http.StatusCreated)
}

// getManifest answers GET (metadata + body) and HEAD (metadata only), resolving
// a tag to its digest first when the reference is not itself a digest.
func (h *handler) getManifest(w http.ResponseWriter, r *http.Request, projectID, repo string, p parsedPath, body bool) {
	digest := p.ref
	if isDigestRef(p.ref) {
		if err := blob.ValidateDigest(p.ref); err != nil {
			writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "invalid digest")
			return
		}
	} else {
		resolved, err := h.manifests.resolveTag(r.Context(), projectID, repo, p.ref)
		if err != nil {
			if errors.Is(err, ErrManifestNotFound) {
				writeError(w, h.log, http.StatusNotFound, codeManifestUnknown, "manifest unknown")
				return
			}
			h.internalError(w, "resolving tag", err)
			return
		}
		digest = resolved
	}

	m, err := h.manifests.getManifest(r.Context(), projectID, repo, digest)
	if err != nil {
		if errors.Is(err, ErrManifestNotFound) {
			writeError(w, h.log, http.StatusNotFound, codeManifestUnknown, "manifest unknown")
			return
		}
		h.internalError(w, "getting manifest", err)
		return
	}

	w.Header().Set("Docker-Content-Digest", m.Digest)
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(m.Size, 10))
	if !body {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(m.Payload); err != nil {
		h.log.Error("streaming manifest", slog.String("error", err.Error()))
	}
}

// deleteManifest removes a manifest (by digest) or untags it (by tag).
func (h *handler) deleteManifest(w http.ResponseWriter, r *http.Request, projectID, repo string, p parsedPath) {
	actor := usernameFromContext(r.Context())

	var err error
	if isDigestRef(p.ref) {
		if verr := blob.ValidateDigest(p.ref); verr != nil {
			writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "invalid digest")
			return
		}
		err = h.manifests.deleteManifest(r.Context(), projectID, repo, p.ref, actor)
	} else {
		if !validTag(p.ref) {
			writeError(w, h.log, http.StatusBadRequest, codeManifestInvalid, "invalid tag")
			return
		}
		err = h.manifests.deleteTag(r.Context(), projectID, repo, p.ref, actor)
	}
	if err != nil {
		if errors.Is(err, ErrManifestNotFound) {
			writeError(w, h.log, http.StatusNotFound, codeManifestUnknown, "manifest unknown")
			return
		}
		h.internalError(w, "deleting manifest", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// validateReferences rejects a manifest that points at content the registry does
// not hold: an image manifest's config and layers must be present as blobs; an
// index's child manifests must already be stored. This is what makes a dangling
// push fail loudly instead of producing an unpullable image.
func (h *handler) validateReferences(ctx context.Context, projectID, repo, mediaType string, doc manifestDoc) error {
	if imageIndexTypes[mediaType] {
		for _, d := range doc.Manifests {
			if err := blob.ValidateDigest(d.Digest); err != nil {
				return &manifestError{http.StatusBadRequest, codeManifestInvalid, "invalid digest in index: " + d.Digest}
			}
			ok, err := h.manifests.manifestExists(ctx, projectID, repo, d.Digest)
			if err != nil {
				return err
			}
			if !ok {
				return &manifestError{http.StatusBadRequest, codeManifestBlobUnknown, "referenced manifest is unknown: " + d.Digest}
			}
		}
		return nil
	}

	// Image manifest: config plus each layer must exist in the blob store. A
	// non-distributable layer carries external urls and lives outside the
	// registry (image-spec), so its blob is not required to be present.
	refs := make([]string, 0, len(doc.Layers)+1)
	if doc.Config != nil && doc.Config.Digest != "" {
		refs = append(refs, doc.Config.Digest)
	}
	for _, l := range doc.Layers {
		if len(l.URLs) > 0 {
			continue
		}
		refs = append(refs, l.Digest)
	}
	for _, d := range refs {
		if err := blob.ValidateDigest(d); err != nil {
			return &manifestError{http.StatusBadRequest, codeManifestInvalid, "invalid digest in manifest: " + d}
		}
		if _, err := h.blobs.Stat(ctx, d); err != nil {
			if errors.Is(err, blob.ErrNotFound) {
				return &manifestError{http.StatusBadRequest, codeManifestBlobUnknown, "referenced blob is unknown: " + d}
			}
			return err
		}
	}
	return nil
}

// resolveName splits the OCI name into <project>/<repository> and resolves the
// project. The scheme (docs/ARCHITECTURE.md) puts the project first; a bare
// single-component name has no project to scope to.
func (h *handler) resolveName(w http.ResponseWriter, r *http.Request, name string) (projectID, repo string, ok bool) {
	key, repo, split := splitName(name)
	if !split {
		writeError(w, h.log, http.StatusNotFound, codeNameUnknown, "repository name must be <project>/<repository>")
		return "", "", false
	}
	projectID, err := h.manifests.resolveProject(r.Context(), key)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, h.log, http.StatusNotFound, codeNameUnknown, fmt.Sprintf("project %q does not exist", key))
			return "", "", false
		}
		h.internalError(w, "resolving project", err)
		return "", "", false
	}
	return projectID, repo, true
}

func usernameFromContext(ctx context.Context) string {
	if u, ok := userFromContext(ctx); ok {
		return u.Username
	}
	return ""
}

// manifestMediaType chooses the media type from the Content-Type header, falling
// back to the document's own mediaType field, accepting only types we know.
func manifestMediaType(header, embedded string) string {
	if ct := normalizeMediaType(header); imageManifestTypes[ct] || imageIndexTypes[ct] {
		return ct
	}
	if imageManifestTypes[embedded] || imageIndexTypes[embedded] {
		return embedded
	}
	return ""
}

// normalizeMediaType strips any parameters (e.g. "; charset=utf-8") and spaces.
func normalizeMediaType(v string) string {
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
