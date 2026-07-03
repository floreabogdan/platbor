package oci

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
)

// serveBlob handles HEAD/GET/DELETE on /v2/<name>/blobs/<digest>.
func (h *handler) serveBlob(w http.ResponseWriter, r *http.Request, p parsedPath) {
	switch r.Method {
	case http.MethodHead:
		h.statBlob(w, r, p.ref, false)
	case http.MethodGet:
		h.statBlob(w, r, p.ref, true)
	case http.MethodDelete:
		// Inline deletion is unsafe for a shared content-addressable store;
		// removal is the job of garbage collection.
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "blob deletion is handled by garbage collection")
	default:
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
	}
}

// statBlob answers HEAD (metadata only) and GET (metadata + content) for a blob.
func (h *handler) statBlob(w http.ResponseWriter, r *http.Request, digest string, body bool) {
	if err := blob.ValidateDigest(digest); err != nil {
		writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "invalid digest")
		return
	}
	desc, err := h.blobs.Stat(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, h.log, http.StatusNotFound, codeBlobUnknown, "blob unknown")
			return
		}
		h.internalError(w, "stat blob", err)
		return
	}

	w.Header().Set("Docker-Content-Digest", desc.Digest)
	w.Header().Set("Content-Type", "application/octet-stream")
	if !body {
		w.Header().Set("Content-Length", strconv.FormatInt(desc.Size, 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		h.internalError(w, "open blob", err)
		return
	}
	defer func() { _ = rc.Close() }()

	// A seekable blob gets Range support and Content-Length for free.
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(desc.Size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming blob", slog.String("error", err.Error()))
	}
}

// serveUpload handles the resumable upload endpoints under
// /v2/<name>/blobs/uploads[/<id>].
func (h *handler) serveUpload(w http.ResponseWriter, r *http.Request, p parsedPath) {
	if p.ref == "" {
		if r.Method == http.MethodPost {
			h.startUpload(w, r, p)
			return
		}
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
		return
	}

	switch r.Method {
	case http.MethodPatch:
		h.patchUpload(w, r, p)
	case http.MethodPut:
		h.finishUpload(w, r, p)
	case http.MethodGet:
		h.uploadStatus(w, r, p)
	case http.MethodDelete:
		h.cancelUpload(w, r, p)
	default:
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
	}
}

// startUpload begins a session (202) or, when a ?digest is supplied, performs a
// single-request monolithic upload (201).
func (h *handler) startUpload(w http.ResponseWriter, r *http.Request, p parsedPath) {
	if digest := r.URL.Query().Get("digest"); digest != "" {
		h.monolithicUpload(w, r, p, digest)
		return
	}

	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		h.internalError(w, "start upload", err)
		return
	}
	defer func() { _ = up.Close() }()

	h.setUploadHeaders(w, p.name, up.ID(), up.Size())
	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) monolithicUpload(w http.ResponseWriter, r *http.Request, p parsedPath, digest string) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		h.internalError(w, "start upload", err)
		return
	}
	if _, err := io.Copy(up, r.Body); err != nil {
		_ = up.Abort(r.Context())
		h.internalError(w, "buffering blob", err)
		return
	}
	h.commit(w, r, up, p.name, digest)
}

func (h *handler) patchUpload(w http.ResponseWriter, r *http.Request, p parsedPath) {
	up, err := h.blobs.ResumeUpload(r.Context(), p.ref)
	if err != nil {
		h.uploadLookupError(w, err)
		return
	}
	defer func() { _ = up.Close() }()

	if cr := r.Header.Get("Content-Range"); cr != "" {
		if start, ok := parseContentRange(cr); !ok || start != up.Size() {
			writeError(w, h.log, http.StatusRequestedRangeNotSatisfiable, codeBlobUploadInvalid, "content range does not match upload offset")
			return
		}
	}
	if _, err := io.Copy(up, r.Body); err != nil {
		h.internalError(w, "appending chunk", err)
		return
	}

	h.setUploadHeaders(w, p.name, p.ref, up.Size())
	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) finishUpload(w http.ResponseWriter, r *http.Request, p parsedPath) {
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "digest query parameter is required")
		return
	}
	up, err := h.blobs.ResumeUpload(r.Context(), p.ref)
	if err != nil {
		h.uploadLookupError(w, err)
		return
	}
	// A closing PUT may carry the final chunk with a Content-Range; if that range
	// does not continue from the current offset the request is out of order and
	// the spec requires 416, not a later digest mismatch.
	if cr := r.Header.Get("Content-Range"); cr != "" {
		if start, ok := parseContentRange(cr); !ok || start != up.Size() {
			_ = up.Close()
			writeError(w, h.log, http.StatusRequestedRangeNotSatisfiable, codeBlobUploadInvalid, "content range does not match upload offset")
			return
		}
	}
	if _, err := io.Copy(up, r.Body); err != nil {
		_ = up.Close()
		h.internalError(w, "appending final chunk", err)
		return
	}
	h.commit(w, r, up, p.name, digest)
}

// commit finalizes an upload against the expected digest and writes the 201.
func (h *handler) commit(w http.ResponseWriter, r *http.Request, up blob.Upload, name, digest string) {
	desc, err := up.Commit(r.Context(), digest)
	if err != nil {
		switch {
		case errors.Is(err, blob.ErrDigestMismatch), errors.Is(err, blob.ErrInvalidDigest):
			writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "content does not match digest")
		default:
			h.internalError(w, "committing blob", err)
		}
		return
	}

	if user, ok := userFromContext(r.Context()); ok {
		h.log.Debug("blob stored", slog.String("digest", desc.Digest), slog.String("repo", name), slog.String("by", user.Username))
	}
	w.Header().Set("Location", "/v2/"+name+"/blobs/"+desc.Digest)
	w.Header().Set("Docker-Content-Digest", desc.Digest)
	w.WriteHeader(http.StatusCreated)
}

func (h *handler) uploadStatus(w http.ResponseWriter, r *http.Request, p parsedPath) {
	up, err := h.blobs.ResumeUpload(r.Context(), p.ref)
	if err != nil {
		h.uploadLookupError(w, err)
		return
	}
	defer func() { _ = up.Close() }()

	h.setUploadHeaders(w, p.name, p.ref, up.Size())
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) cancelUpload(w http.ResponseWriter, r *http.Request, p parsedPath) {
	up, err := h.blobs.ResumeUpload(r.Context(), p.ref)
	if err != nil {
		h.uploadLookupError(w, err)
		return
	}
	if err := up.Abort(r.Context()); err != nil {
		h.internalError(w, "canceling upload", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setUploadHeaders writes the Location/Range/UUID headers common to upload
// responses. Range is inclusive; an empty upload reports "0-0".
func (h *handler) setUploadHeaders(w http.ResponseWriter, name, id string, size int64) {
	end := size - 1
	if end < 0 {
		end = 0
	}
	w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+id)
	w.Header().Set("Docker-Upload-UUID", id)
	w.Header().Set("Range", "0-"+strconv.FormatInt(end, 10))
}

func (h *handler) uploadLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, blob.ErrUploadNotFound) {
		writeError(w, h.log, http.StatusNotFound, codeBlobUploadUnknown, "upload unknown")
		return
	}
	h.internalError(w, "resolving upload", err)
}
