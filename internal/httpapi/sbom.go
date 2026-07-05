package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/registry/oci"
)

// maxSBOMBytes caps how much of an SBOM document we read and parse.
const maxSBOMBytes = 32 << 20 // 32 MiB

// sbomComponent is one package/library listed in an SBOM.
type sbomComponent struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	License string `json:"license,omitempty"`
	Type    string `json:"type,omitempty"`
	// PURL is the package URL (pkg:npm/left-pad@1.0.0). It identifies the package
	// to a vulnerability database; components without one cannot be scanned.
	PURL string `json:"purl,omitempty"`
}

type sbomResponse struct {
	Format     string          `json:"format"` // "cyclonedx" | "spdx"
	Components []sbomComponent `json:"components"`
}

// getSBOM reads and parses an SBOM attached to an image via the referrers API. It
// resolves the referrer manifest by digest, opens its document layer, and returns
// the component list. Supports CycloneDX and SPDX JSON — the two the ecosystem
// (cosign, syft, docker sbom) produces.
func (h registryHandler) getSBOM(w http.ResponseWriter, r *http.Request) {
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeProblem(w, http.StatusBadRequest, "Missing digest", "the digest query parameter is required")
		return
	}

	// The referrer is itself a manifest; its document is its (single) layer.
	view, err := h.browser.Manifest(r.Context(), repo.ID, image, digest)
	if err != nil {
		if errors.Is(err, oci.ErrManifestNotFound) {
			writeProblem(w, http.StatusNotFound, "SBOM not found", "no manifest for that digest")
			return
		}
		h.log.Error("getting sbom manifest", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	layer, ok := sbomLayer(view)
	if !ok {
		writeProblem(w, http.StatusUnprocessableEntity, "Not an SBOM", "the referrer has no SBOM document layer")
		return
	}

	data, err := h.readBlob(r, layer.Digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "SBOM blob missing", "the SBOM document is not stored")
			return
		}
		h.log.Error("reading sbom blob", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	resp, err := parseSBOM(data)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Unrecognized SBOM", err.Error())
		return
	}
	writeJSON(w, h.log, http.StatusOK, resp)
}

// sbomLayer picks the document layer of an SBOM referrer: the one whose media
// type looks like an SBOM, else the single/first layer.
func sbomLayer(view oci.ManifestView) (oci.LayerRef, bool) {
	if len(view.Layers) == 0 {
		return oci.LayerRef{}, false
	}
	for _, l := range view.Layers {
		if isSBOMMediaType(l.MediaType) {
			return l, true
		}
	}
	return view.Layers[0], true
}

func isSBOMMediaType(mt string) bool {
	mt = strings.ToLower(mt)
	return strings.Contains(mt, "spdx") || strings.Contains(mt, "cyclonedx") || strings.Contains(mt, "sbom")
}

// readBlob reads a stored blob fully, bounded by maxSBOMBytes.
func (h registryHandler) readBlob(r *http.Request, digest string) ([]byte, error) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, maxSBOMBytes))
}

// --- parsing ---

// parseSBOM detects and parses a CycloneDX or SPDX JSON document.
func parseSBOM(data []byte) (sbomResponse, error) {
	var probe struct {
		BOMFormat   string          `json:"bomFormat"`
		SPDXVersion string          `json:"spdxVersion"`
		Components  json.RawMessage `json:"components"`
		Packages    json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return sbomResponse{}, errors.New("SBOM is not valid JSON")
	}
	switch {
	case strings.EqualFold(probe.BOMFormat, "CycloneDX") || len(probe.Components) > 0:
		return parseCycloneDX(data)
	case probe.SPDXVersion != "" || len(probe.Packages) > 0:
		return parseSPDX(data)
	default:
		return sbomResponse{}, errors.New("unrecognized SBOM format (expected CycloneDX or SPDX JSON)")
	}
}

func parseCycloneDX(data []byte) (sbomResponse, error) {
	var doc struct {
		Components []struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Type     string `json:"type"`
			PURL     string `json:"purl"`
			Licenses []struct {
				License struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"license"`
				Expression string `json:"expression"`
			} `json:"licenses"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return sbomResponse{}, errors.New("invalid CycloneDX document")
	}
	comps := make([]sbomComponent, 0, len(doc.Components))
	for _, c := range doc.Components {
		if c.Name == "" {
			continue
		}
		lic := ""
		if len(c.Licenses) > 0 {
			l := c.Licenses[0]
			switch {
			case l.License.ID != "":
				lic = l.License.ID
			case l.License.Name != "":
				lic = l.License.Name
			default:
				lic = l.Expression
			}
		}
		comps = append(comps, sbomComponent{Name: c.Name, Version: c.Version, License: lic, Type: c.Type, PURL: c.PURL})
	}
	return sbomResponse{Format: "cyclonedx", Components: dedupeSort(comps)}, nil
}

func parseSPDX(data []byte) (sbomResponse, error) {
	var doc struct {
		Packages []struct {
			Name             string `json:"name"`
			VersionInfo      string `json:"versionInfo"`
			LicenseConcluded string `json:"licenseConcluded"`
			LicenseDeclared  string `json:"licenseDeclared"`
			ExternalRefs     []struct {
				ReferenceType     string `json:"referenceType"`
				ReferenceCategory string `json:"referenceCategory"`
				ReferenceLocator  string `json:"referenceLocator"`
			} `json:"externalRefs"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return sbomResponse{}, errors.New("invalid SPDX document")
	}
	comps := make([]sbomComponent, 0, len(doc.Packages))
	for _, p := range doc.Packages {
		if p.Name == "" {
			continue
		}
		lic := p.LicenseConcluded
		if lic == "" || lic == "NOASSERTION" {
			lic = p.LicenseDeclared
		}
		if lic == "NOASSERTION" {
			lic = ""
		}
		purl := ""
		for _, ref := range p.ExternalRefs {
			if strings.EqualFold(ref.ReferenceType, "purl") {
				purl = ref.ReferenceLocator
				break
			}
		}
		comps = append(comps, sbomComponent{Name: p.Name, Version: p.VersionInfo, License: lic, Type: "library", PURL: purl})
	}
	return sbomResponse{Format: "spdx", Components: dedupeSort(comps)}, nil
}

// dedupeSort removes duplicate (name, version) entries and orders by name then
// version so the list is stable and legible.
func dedupeSort(comps []sbomComponent) []sbomComponent {
	seen := make(map[string]struct{}, len(comps))
	out := comps[:0]
	for _, c := range comps {
		key := c.Name + "\x00" + c.Version
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}
