package oci

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/platbor/platbor/internal/core/blob"
)

// ociImageIndexV1 is the media type of the referrers response — an image index
// whose entries are the manifests referring to the subject.
const ociImageIndexV1 = "application/vnd.oci.image.index.v1+json"

// ociDescriptor is one entry in the referrers index: the referring manifest's
// descriptor plus its artifact type and annotations, which is what discovery
// tools (cosign, SBOM readers) match on.
type ociDescriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// referrersResponse is the image index returned by the referrers API.
type referrersResponse struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []ociDescriptor `json:"manifests"`
}

// serveReferrers implements GET /v2/<name>/referrers/<digest>: an image index of
// every manifest whose subject is <digest>, optionally filtered by artifactType.
// It always answers 200 with an index — an unreferenced subject yields an empty
// one — per the distribution spec.
func (h *handler) serveReferrers(w http.ResponseWriter, r *http.Request, p parsedPath) {
	if r.Method != http.MethodGet {
		writeError(w, h.log, http.StatusMethodNotAllowed, codeUnsupported, "method not allowed")
		return
	}
	if err := blob.ValidateDigest(p.ref); err != nil {
		writeError(w, h.log, http.StatusBadRequest, codeDigestInvalid, "invalid subject digest")
		return
	}

	projectID, repo, ok := h.resolveName(w, r, p.name)
	if !ok {
		return
	}

	rows, err := h.manifests.listReferrers(r.Context(), projectID, repo, p.ref)
	if err != nil {
		h.internalError(w, "listing referrers", err)
		return
	}

	filter := r.URL.Query().Get("artifactType")
	index := referrersResponse{
		SchemaVersion: 2,
		MediaType:     ociImageIndexV1,
		Manifests:     make([]ociDescriptor, 0, len(rows)),
	}
	for _, row := range rows {
		if filter != "" && row.ArtifactType != filter {
			continue
		}
		index.Manifests = append(index.Manifests, ociDescriptor{
			MediaType:    row.MediaType,
			Digest:       row.Digest,
			Size:         row.Size,
			ArtifactType: row.ArtifactType,
			Annotations:  annotationsFromPayload(row.Payload),
		})
	}

	body, err := json.Marshal(index)
	if err != nil {
		h.internalError(w, "encoding referrers index", err)
		return
	}

	w.Header().Set("Content-Type", ociImageIndexV1)
	// Advertise that the artifactType filter was honored so clients need not
	// re-filter (distribution spec: OCI-Filters-Applied).
	if filter != "" {
		w.Header().Set("OCI-Filters-Applied", "artifactType")
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.Error("writing referrers index", slog.String("error", err.Error()))
	}
}

// annotationsFromPayload extracts a manifest's top-level annotations, or nil.
func annotationsFromPayload(payload []byte) map[string]string {
	var doc manifestDoc
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil
	}
	return doc.Annotations
}
