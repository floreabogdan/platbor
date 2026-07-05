// Package scan matches an artifact's SBOM component inventory against the OSV
// vulnerability database and records the findings. It is SBOM-driven and needs
// no external scanner binary or vulnerability-database download: OSV is queried
// on demand over HTTPS only when a scan is triggered, so the single-binary,
// no-required-service promise holds. The results are stored so they can be read
// both ways — an artifact's vulnerabilities, and a vulnerability's affected
// artifacts (the "CVE -> artifacts" query).
//
// This package sits beside registry in the dependency graph (httpapi ->
// {registry, catalog, scan} -> core): it depends only on core and never imports a
// format adapter. The caller (httpapi) fetches an artifact's SBOM components and
// hands them here as plain structs.
package scan

import (
	"context"
	"sort"
	"strings"
)

const (
	defaultOSVEndpoint = "https://api.osv.dev"
	userAgent          = "platbor/1.0"
	maxOSVResponse     = 16 << 20 // 16 MiB cap on any single OSV response
)

// Severity buckets, ordered worst to best.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityUnknown  = "unknown"
)

// severityRank orders severities so the worst sorts first and the rollup can pick
// the highest without parsing labels.
func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// Component is one entry from an artifact's SBOM. Only components carrying a
// package URL (purl) can be scanned — the purl is what identifies the package to
// OSV (name + ecosystem + version).
type Component struct {
	Name    string
	Version string
	PURL    string
}

// Finding is one vulnerability affecting one component.
type Finding struct {
	VulnID       string // preferred identifier (CVE when available, else OSV id)
	Package      string
	Version      string
	Ecosystem    string
	Severity     string // critical|high|medium|low|unknown
	SeverityRank int
	Summary      string
	FixedVersion string
	ReferenceURL string
}

// Result is a completed scan of a component set.
type Result struct {
	ComponentCount int            // components that carried a purl and were scanned
	Findings       []Finding      // deduped, worst severity first
	Counts         map[string]int // severity -> count
}

// Scanner runs vulnerability scans against OSV.
type Scanner struct {
	osv *osvClient
}

// NewScanner builds a scanner. endpoint overrides the OSV base URL (empty uses
// the public api.osv.dev), which the tests point at a stub server.
func NewScanner(endpoint string) *Scanner {
	return &Scanner{osv: newOSVClient(endpoint)}
}

// Scan matches the components against OSV and returns the findings. Components
// without a purl are skipped (OSV cannot identify them). A nil/empty component
// set yields an empty result without contacting OSV.
func (s *Scanner) Scan(ctx context.Context, components []Component) (Result, error) {
	// Deduplicate purls (an SBOM can list a package more than once) while keeping
	// each scannable component so a finding is attributed to every occurrence.
	scannable := make([]Component, 0, len(components))
	purlSet := map[string]struct{}{}
	var purls []string
	for _, c := range components {
		if strings.TrimSpace(c.PURL) == "" {
			continue
		}
		scannable = append(scannable, c)
		if _, ok := purlSet[c.PURL]; !ok {
			purlSet[c.PURL] = struct{}{}
			purls = append(purls, c.PURL)
		}
	}
	result := Result{
		ComponentCount: len(scannable),
		Findings:       []Finding{},
		Counts:         map[string]int{},
	}
	if len(purls) == 0 {
		return result, nil
	}

	batch, err := s.osv.queryBatch(ctx, purls)
	if err != nil {
		return Result{}, err
	}
	purlVulns := make(map[string][]string, len(purls))
	uniqueIDs := map[string]struct{}{}
	var idList []string
	for i, ids := range batch {
		purlVulns[purls[i]] = ids
		for _, id := range ids {
			if _, ok := uniqueIDs[id]; !ok {
				uniqueIDs[id] = struct{}{}
				idList = append(idList, id)
			}
		}
	}
	if len(idList) == 0 {
		return result, nil
	}

	vulns, err := s.osv.hydrate(ctx, idList)
	if err != nil {
		return Result{}, err
	}

	seen := map[string]struct{}{}
	for _, c := range scannable {
		for _, rawID := range purlVulns[c.PURL] {
			v := vulns[rawID]
			if v == nil {
				continue
			}
			eco := ecosystemFromPURL(c.PURL)
			f := Finding{
				VulnID:       preferredID(v),
				Package:      c.Name,
				Version:      c.Version,
				Ecosystem:    eco,
				Severity:     severityOf(v),
				Summary:      summaryOf(v),
				FixedVersion: fixedVersionFor(v, eco, c.Name),
				ReferenceURL: referenceURL(v),
			}
			f.SeverityRank = severityRank(f.Severity)
			key := f.VulnID + "\x00" + f.Package + "\x00" + f.Version
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			result.Findings = append(result.Findings, f)
			result.Counts[f.Severity]++
		}
	}

	sort.Slice(result.Findings, func(i, j int) bool {
		a, b := result.Findings[i], result.Findings[j]
		if a.SeverityRank != b.SeverityRank {
			return a.SeverityRank > b.SeverityRank
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.VulnID < b.VulnID
	})
	return result, nil
}

// summaryOf prefers the one-line summary, falling back to a trimmed details blob.
func summaryOf(v *osvVuln) string {
	if v.Summary != "" {
		return v.Summary
	}
	d := strings.TrimSpace(v.Details)
	if len(d) > 300 {
		d = d[:300] + "..."
	}
	return d
}

// ecosystemFromPURL maps a package URL's type to a human ecosystem label for
// display and fixed-version matching (e.g. "pkg:npm/left-pad@1.0.0" -> "npm").
// OSV itself infers the ecosystem from the purl; this label is for our own use.
func ecosystemFromPURL(purl string) string {
	rest := strings.TrimPrefix(purl, "pkg:")
	typ, _, _ := strings.Cut(rest, "/")
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "npm":
		return "npm"
	case "pypi":
		return "PyPI"
	case "gem":
		return "RubyGems"
	case "cargo":
		return "crates.io"
	case "golang":
		return "Go"
	case "maven":
		return "Maven"
	case "nuget":
		return "NuGet"
	case "composer":
		return "Packagist"
	case "apk":
		return "Alpine"
	case "deb":
		return "Debian"
	case "rpm":
		return "RPM"
	default:
		return typ
	}
}
