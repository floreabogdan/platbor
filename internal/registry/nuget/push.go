package nuget

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// maxNupkgSize caps a single package upload.
const maxNupkgSize = 1 << 30 // 1 GiB

// push handles `dotnet nuget push`: read the .nupkg (multipart form file, or a
// raw body), extract its id/version from the embedded .nuspec, store the package
// in the blob store, and index the version. Proxy feeds are read-only.
func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot push to a proxy feed")
		return
	}

	nupkg, err := readPackage(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading package: "+err.Error())
		return
	}

	pkgID, version, nuspec, err := parseNupkg(nupkg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid package: "+err.Error())
		return
	}

	digest, size, err := h.storeBlob(r, nupkg)
	if err != nil {
		h.internalError(w, "storing package", err)
		return
	}

	if err := h.store.push(r.Context(), pushInput{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		IDOriginal:   pkgID,
		Version:      version,
		NupkgDigest:  digest,
		NupkgSize:    size,
		Nuspec:       nuspec,
		Actor:        actorFrom(r),
	}); err != nil {
		if errors.Is(err, ErrVersionExists) {
			// NuGet uses 409 for a duplicate version.
			writeError(w, http.StatusConflict, "package version already exists")
			return
		}
		h.internalError(w, "indexing package", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// readPackage extracts the .nupkg bytes: from the first multipart file part when
// the request is multipart/form-data (what the NuGet client sends), otherwise
// from the raw request body.
func readPackage(r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, err
		}
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("no package part in upload")
			}
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(part, maxNupkgSize))
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			if len(data) > 0 {
				return data, nil
			}
		}
	}
	return io.ReadAll(io.LimitReader(r.Body, maxNupkgSize))
}

// storeBlob commits the package bytes to the content-addressable store.
func (h *handler) storeBlob(r *http.Request, data []byte) (string, int64, error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, fmt.Errorf("writing package: %w", err)
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, fmt.Errorf("committing package: %w", err)
	}
	return desc.Digest, desc.Size, nil
}
