package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/scan"
)

// vulnerabilitiesHandler serves the instance-wide vulnerability index: the
// "CVE -> artifacts" query that turns scan findings inside out. It reads from the
// stored scan findings, so it is available whether or not scanning is currently
// enabled.
type vulnerabilitiesHandler struct {
	scans *scan.Service
	log   *slog.Logger
}

func (h vulnerabilitiesHandler) mount(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/{id}", h.affected)
}

type vulnerabilityResponse struct {
	VulnID        string `json:"vulnId"`
	Severity      string `json:"severity"`
	Summary       string `json:"summary,omitempty"`
	ArtifactCount int    `json:"artifactCount"`
}

type listVulnerabilitiesResponse struct {
	Vulnerabilities []vulnerabilityResponse `json:"vulnerabilities"`
}

func (h vulnerabilitiesHandler) list(w http.ResponseWriter, r *http.Request) {
	vulns, err := h.scans.Vulnerabilities(r.Context())
	if err != nil {
		h.log.Error("listing vulnerabilities", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]vulnerabilityResponse, 0, len(vulns))
	for _, v := range vulns {
		items = append(items, vulnerabilityResponse{
			VulnID:        v.VulnID,
			Severity:      v.Severity,
			Summary:       v.Summary,
			ArtifactCount: v.ArtifactCount,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listVulnerabilitiesResponse{Vulnerabilities: items})
}

type affectedArtifactResponse struct {
	ProjectKey   string    `json:"projectKey"`
	ProjectName  string    `json:"projectName"`
	RepoKey      string    `json:"repoKey"`
	Image        string    `json:"image"`
	Digest       string    `json:"digest"`
	Package      string    `json:"package"`
	Version      string    `json:"version"`
	Severity     string    `json:"severity"`
	FixedVersion string    `json:"fixedVersion,omitempty"`
	ScannedAt    time.Time `json:"scannedAt"`
}

type affectedResponse struct {
	VulnID   string                     `json:"vulnId"`
	Affected []affectedArtifactResponse `json:"affected"`
}

func (h vulnerabilitiesHandler) affected(w http.ResponseWriter, r *http.Request) {
	vulnID := chi.URLParam(r, "id")
	if vulnID == "" {
		writeProblem(w, http.StatusBadRequest, "Missing id", "a vulnerability id is required")
		return
	}
	rows, err := h.scans.AffectedBy(r.Context(), vulnID)
	if err != nil {
		h.log.Error("listing affected artifacts", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]affectedArtifactResponse, 0, len(rows))
	for _, a := range rows {
		items = append(items, affectedArtifactResponse{
			ProjectKey:   a.ProjectKey,
			ProjectName:  a.ProjectName,
			RepoKey:      a.RepoKey,
			Image:        a.Image,
			Digest:       a.Digest,
			Package:      a.Package,
			Version:      a.Version,
			Severity:     a.Severity,
			FixedVersion: a.FixedVersion,
			ScannedAt:    a.ScannedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, affectedResponse{VulnID: vulnID, Affected: items})
}
