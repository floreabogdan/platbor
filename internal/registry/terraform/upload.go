package terraform

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// upload handles the Platbor-specific module upload:
//
//	PUT /terraform/upload/<project>/<repo>/<name>/<provider>/<version>
//
// with the module archive (a tar.gz) as the raw body. Terraform has no standard
// module upload API, so this is how modules get into a local repository. The
// namespace terraform later uses to fetch the module is the project key. A
// re-upload of an existing version is a 409.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	repoKey := chi.URLParam(r, "repo")
	name := chi.URLParam(r, "name")
	provider := chi.URLParam(r, "provider")
	version := chi.URLParam(r, "version")
	if name == "" || provider == "" || version == "" {
		writeError(w, http.StatusBadRequest, "name, provider and version are required")
		return
	}

	repo, ok := h.resolveUploadRepo(w, r, project, repoKey)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot upload to a proxy repository")
		return
	}

	upload, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		h.internalError(w, "starting upload", err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxArchiveSize))
	if err != nil {
		_ = upload.Abort(r.Context())
		h.internalError(w, "reading archive", err)
		return
	}
	if _, err := upload.Write(data); err != nil {
		_ = upload.Abort(r.Context())
		h.internalError(w, "buffering archive", err)
		return
	}
	desc, err := upload.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		h.internalError(w, "committing archive", err)
		return
	}

	if err := h.store.upload(r.Context(), uploadInput{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		Name:         name,
		Provider:     provider,
		Version:      version,
		BlobDigest:   desc.Digest,
		Size:         desc.Size,
		Actor:        actorFrom(r),
	}); err != nil {
		if errors.Is(err, ErrVersionExists) {
			writeError(w, http.StatusConflict, "module version already exists: "+name+"/"+provider+"@"+version)
			return
		}
		h.internalError(w, "recording module", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
