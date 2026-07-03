package rubygems

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// maxGemSize caps a single pushed .gem.
const maxGemSize = 1 << 30 // 1 GiB

// push handles `gem push`: a POST whose raw body is the .gem file. The gemspec is
// parsed for the compact-index fields, the .gem stored in the blob store. Proxy
// repos are read-only; a re-push of an existing version is a 409.
func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot push to a proxy repository")
		return
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, maxGemSize))
	if err != nil {
		h.internalError(w, "reading gem", err)
		return
	}
	sum := sha256.Sum256(data)
	cksum := hex.EncodeToString(sum[:])
	spec, err := parseGemSpec(data, cksum)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid gem: "+err.Error())
		return
	}

	digest, size, err := h.storeBlob(r, data)
	if err != nil {
		h.internalError(w, "storing gem", err)
		return
	}

	if err := h.store.push(r.Context(), pushInput{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		Spec:         spec,
		BlobDigest:   digest,
		Size:         size,
		Actor:        actorFrom(r),
	}); err != nil {
		if errors.Is(err, ErrVersionExists) {
			writeError(w, http.StatusConflict, "Repushing of gem versions is not allowed.\nPlease use `gem yank` to remove bad gems.")
			return
		}
		h.internalError(w, "recording gem", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "Successfully registered gem: "+spec.Name+" ("+spec.Number+")\n")
}

// storeBlob commits bytes to the content-addressable store.
func (h *handler) storeBlob(r *http.Request, data []byte) (string, int64, error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, err
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}
