package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// maxFileSize caps a single distribution upload.
const maxFileSize = 2 << 30 // 2 GiB

// upload handles `twine upload`: a multipart/form-data POST carrying the
// distribution file under "content" plus metadata fields (name, version,
// sha256_digest, requires_python). The file is stored in the blob store and
// indexed. Proxy repositories are read-only. Re-uploading a filename is a 409.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot upload to a proxy repository")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload: "+err.Error())
		return
	}
	name := r.FormValue("name")
	version := r.FormValue("version")
	if name == "" || version == "" {
		writeError(w, http.StatusBadRequest, "missing name or version")
		return
	}

	part, header, err := r.FormFile("content")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing distribution file (content)")
		return
	}
	defer func() { _ = part.Close() }()
	data, err := io.ReadAll(io.LimitReader(part, maxFileSize))
	if err != nil {
		h.internalError(w, "reading upload", err)
		return
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if provided := r.FormValue("sha256_digest"); provided != "" && !strings.EqualFold(provided, sha) {
		writeError(w, http.StatusBadRequest, "sha256 digest mismatch")
		return
	}

	digest, size, err := h.storeBlob(r, data)
	if err != nil {
		h.internalError(w, "storing distribution", err)
		return
	}

	if err := h.store.upload(r.Context(), uploadInput{
		RepositoryID:   repo.ID,
		ProjectID:      repo.ProjectID,
		NameNormalized: normalizeName(name),
		NameOriginal:   name,
		Version:        version,
		Filename:       header.Filename,
		BlobDigest:     digest,
		Size:           size,
		SHA256:         sha,
		RequiresPython: r.FormValue("requires_python"),
		Actor:          actorFrom(r),
	}); err != nil {
		if errors.Is(err, ErrFileExists) {
			writeError(w, http.StatusConflict, "file already exists: "+header.Filename)
			return
		}
		h.internalError(w, "indexing distribution", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// storeBlob commits the distribution bytes to the content-addressable store.
func (h *handler) storeBlob(r *http.Request, data []byte) (string, int64, error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, fmt.Errorf("writing distribution: %w", err)
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, fmt.Errorf("committing distribution: %w", err)
	}
	return desc.Digest, desc.Size, nil
}
