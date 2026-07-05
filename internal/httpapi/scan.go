package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/scan"
)

type scanFindingResponse struct {
	VulnID       string `json:"vulnId"`
	Package      string `json:"package"`
	Version      string `json:"version"`
	Ecosystem    string `json:"ecosystem,omitempty"`
	Severity     string `json:"severity"`
	Summary      string `json:"summary,omitempty"`
	FixedVersion string `json:"fixedVersion,omitempty"`
	ReferenceURL string `json:"referenceUrl,omitempty"`
}

type scanResponse struct {
	Digest         string                `json:"digest"`
	SourceDigest   string                `json:"sourceDigest"`
	ComponentCount int                   `json:"componentCount"`
	Counts         map[string]int        `json:"counts"`
	ScannedAt      time.Time             `json:"scannedAt"`
	Findings       []scanFindingResponse `json:"findings"`
}

// getScan returns the stored vulnerability scan of a manifest, or 404 when it has
// not been scanned yet.
func (h registryHandler) getScan(w http.ResponseWriter, r *http.Request) {
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeProblem(w, http.StatusBadRequest, "Missing digest", "the digest query parameter is required")
		return
	}
	summary, findings, err := h.scans.Latest(r.Context(), repo.ID, image, digest)
	if err != nil {
		if errors.Is(err, scan.ErrNoScan) {
			writeProblem(w, http.StatusNotFound, "Not scanned", "this artifact has not been scanned yet")
			return
		}
		h.log.Error("getting scan", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, toScanResponse(summary, findings))
}

// runScan scans a manifest's SBOM against OSV and stores the result. It locates
// the manifest's SBOM referrer, reads its component inventory, matches it against
// the vulnerability database, and persists the findings so they are queryable both
// ways. Scanning is SBOM-driven — an artifact with no SBOM referrer cannot be
// scanned (422).
func (h registryHandler) runScan(w http.ResponseWriter, r *http.Request) {
	if !h.scanEnabled {
		writeProblem(w, http.StatusServiceUnavailable, "Scanning disabled", "vulnerability scanning is disabled on this instance")
		return
	}
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeProblem(w, http.StatusBadRequest, "Missing digest", "the digest query parameter is required")
		return
	}

	sourceDigest, components, err := h.sbomComponentsFor(r, repo.ID, image, digest)
	if err != nil {
		switch {
		case errors.Is(err, errNoSBOM):
			writeProblem(w, http.StatusUnprocessableEntity, "No SBOM", "attach an SBOM (e.g. `cosign attach sbom`) before scanning")
			return
		case errors.Is(err, blob.ErrNotFound):
			writeProblem(w, http.StatusNotFound, "SBOM blob missing", "the SBOM document is not stored")
			return
		case errors.Is(err, oci.ErrManifestNotFound):
			writeProblem(w, http.StatusNotFound, "Manifest not found", "no manifest for that digest")
			return
		default:
			h.log.Error("reading sbom for scan", "error", err.Error())
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
			return
		}
	}

	result, err := h.scanner.Scan(r.Context(), components)
	if err != nil {
		h.log.Warn("scanning against OSV", "error", err.Error())
		writeProblem(w, http.StatusBadGateway, "Scan failed", "the vulnerability database could not be reached")
		return
	}

	summary, err := h.scans.Save(r.Context(), repo.ProjectID, repo.ID, image, digest, sourceDigest, result, actorFrom(r))
	if err != nil {
		h.log.Error("saving scan", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, toScanResponse(summary, result.Findings))
}

// errNoSBOM signals that a manifest has no SBOM referrer to scan.
var errNoSBOM = errors.New("no SBOM referrer")

// sbomComponentsFor finds a manifest's SBOM referrer, reads it, and returns the
// referrer digest plus the component inventory to scan.
func (h registryHandler) sbomComponentsFor(r *http.Request, repoID, image, subject string) (string, []scan.Component, error) {
	refs, err := h.browser.Referrers(r.Context(), repoID, image, subject)
	if err != nil {
		return "", nil, err
	}
	sbomDigest := ""
	for _, ref := range refs {
		if isSBOMMediaType(ref.ArtifactType) || isSBOMMediaType(ref.MediaType) {
			sbomDigest = ref.Digest
			break
		}
	}
	if sbomDigest == "" {
		return "", nil, errNoSBOM
	}

	view, err := h.browser.Manifest(r.Context(), repoID, image, sbomDigest)
	if err != nil {
		return "", nil, err
	}
	layer, ok := sbomLayer(view)
	if !ok {
		return "", nil, errNoSBOM
	}
	data, err := h.readBlob(r, layer.Digest)
	if err != nil {
		return "", nil, err
	}
	parsed, err := parseSBOM(data)
	if err != nil {
		return "", nil, err
	}
	components := make([]scan.Component, 0, len(parsed.Components))
	for _, c := range parsed.Components {
		if c.PURL == "" {
			continue
		}
		components = append(components, scan.Component{Name: c.Name, Version: c.Version, PURL: c.PURL})
	}
	return sbomDigest, components, nil
}

func toScanResponse(s scan.Scan, findings []scan.Finding) scanResponse {
	items := make([]scanFindingResponse, 0, len(findings))
	for _, f := range findings {
		items = append(items, scanFindingResponse{
			VulnID:       f.VulnID,
			Package:      f.Package,
			Version:      f.Version,
			Ecosystem:    f.Ecosystem,
			Severity:     f.Severity,
			Summary:      f.Summary,
			FixedVersion: f.FixedVersion,
			ReferenceURL: f.ReferenceURL,
		})
	}
	return scanResponse{
		Digest:         s.Digest,
		SourceDigest:   s.SourceDigest,
		ComponentCount: s.ComponentCount,
		Counts:         s.Counts,
		ScannedAt:      s.CreatedAt,
		Findings:       items,
	}
}
