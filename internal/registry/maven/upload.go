package maven

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/platbor/platbor/internal/core/repository"
)

// upload stores a PUT file at its path. `mvn deploy` uploads the pom, jar,
// checksum siblings, and maven-metadata.xml as separate PUTs; each is stored
// verbatim (overwriting, so SNAPSHOT redeploys and metadata updates work). Proxy
// repositories are read-only.
func (h *handler) upload(w http.ResponseWriter, r *http.Request, repo repository.Repository, path string) {
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot upload to a proxy repository")
		return
	}

	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		h.internalError(w, "starting upload", err)
		return
	}
	h256, h1, hm := sha256.New(), sha1.New(), md5.New()
	mw := io.MultiWriter(up, h256, h1, hm)
	size, err := io.Copy(mw, io.LimitReader(r.Body, maxFileSize))
	if err != nil {
		_ = up.Abort(r.Context())
		h.internalError(w, "buffering upload", err)
		return
	}
	desc, err := up.Commit(r.Context(), "sha256:"+hex.EncodeToString(h256.Sum(nil)))
	if err != nil {
		h.internalError(w, "committing blob", err)
		return
	}

	if err := h.store.put(r.Context(), filePut{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		Path:         path,
		BlobDigest:   desc.Digest,
		Size:         size,
		SHA1:         hex.EncodeToString(h1.Sum(nil)),
		MD5:          hex.EncodeToString(hm.Sum(nil)),
		Actor:        actorFrom(r),
		audit:        true,
	}); err != nil {
		h.internalError(w, "recording file", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// remove deletes a file (local only), auditing it.
func (h *handler) remove(w http.ResponseWriter, r *http.Request, repo repository.Repository, path string) {
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot delete from a proxy repository")
		return
	}
	if err := h.store.delete(r.Context(), repo.ID, repo.ProjectID, path, actorFrom(r)); err != nil {
		if err == ErrFileNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "deleting file", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
