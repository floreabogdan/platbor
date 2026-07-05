package scan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// osvClient queries the OSV.dev API (https://osv.dev): a free, no-auth
// vulnerability database keyed by package coordinates. It is contacted only when
// a scan is triggered, so the platform boots and serves fully without it — the
// single-binary, no-required-service promise holds; OSV is an on-demand lookup,
// not a runtime dependency.
type osvClient struct {
	endpoint string
	http     *http.Client
	// hydrateConcurrency bounds parallel per-vulnerability detail fetches.
	hydrateConcurrency int
}

func newOSVClient(endpoint string) *osvClient {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = defaultOSVEndpoint
	}
	return &osvClient{
		endpoint:           endpoint,
		http:               &http.Client{Timeout: 30 * time.Second},
		hydrateConcurrency: 8,
	}
}

// osvBatchQuery is one entry in a /v1/querybatch request. OSV accepts a package
// URL (purl) with the version embedded and infers the ecosystem from it.
type osvBatchQuery struct {
	Package osvPackage `json:"package"`
}

type osvPackage struct {
	PURL string `json:"purl"`
}

// queryBatch resolves each purl to the vulnerability IDs affecting it. The result
// slice is index-aligned with purls. OSV caps a batch at 1000 queries, so larger
// inputs are chunked.
func (c *osvClient) queryBatch(ctx context.Context, purls []string) ([][]string, error) {
	out := make([][]string, 0, len(purls))
	const maxBatch = 1000
	for start := 0; start < len(purls); start += maxBatch {
		end := min(start+maxBatch, len(purls))
		chunk, err := c.queryBatchChunk(ctx, purls[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

func (c *osvClient) queryBatchChunk(ctx context.Context, purls []string) ([][]string, error) {
	queries := make([]osvBatchQuery, len(purls))
	for i, p := range purls {
		queries[i] = osvBatchQuery{Package: osvPackage{PURL: p}}
	}
	body, err := json.Marshal(map[string]any{"queries": queries})
	if err != nil {
		return nil, fmt.Errorf("encoding querybatch: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/querybatch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying OSV: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV querybatch returned %s", resp.Status)
	}

	var decoded struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOSVResponse)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decoding querybatch: %w", err)
	}
	res := make([][]string, len(purls))
	for i := range res {
		if i >= len(decoded.Results) {
			res[i] = nil
			continue
		}
		ids := make([]string, 0, len(decoded.Results[i].Vulns))
		for _, v := range decoded.Results[i].Vulns {
			if v.ID != "" {
				ids = append(ids, v.ID)
			}
		}
		res[i] = ids
	}
	return res, nil
}

// osvVuln is the subset of a hydrated OSV vulnerability record we surface.
type osvVuln struct {
	ID       string   `json:"id"`
	Summary  string   `json:"summary"`
	Details  string   `json:"details"`
	Aliases  []string `json:"aliases"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	Affected []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
			PURL      string `json:"purl"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced string `json:"introduced"`
				Fixed      string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
		} `json:"database_specific"`
	} `json:"affected"`
	References []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"references"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

// hydrate fetches full records for each unique vulnerability ID, concurrently.
func (c *osvClient) hydrate(ctx context.Context, ids []string) (map[string]*osvVuln, error) {
	out := make(map[string]*osvVuln, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, c.hydrateConcurrency)
	errc := make(chan error, len(ids))

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
			defer func() { <-sem }()

			v, err := c.getVuln(ctx, id)
			if err != nil {
				errc <- err
				return
			}
			mu.Lock()
			out[id] = v
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	close(errc)
	if err := <-errc; err != nil {
		return nil, err
	}
	return out, nil
}

func (c *osvClient) getVuln(ctx context.Context, id string) (*osvVuln, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/vulns/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV vuln %s returned %s", id, resp.Status)
	}
	var v osvVuln
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOSVResponse)).Decode(&v); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", id, err)
	}
	return &v, nil
}

// --- severity derivation ---

// severityOf derives a coarse severity bucket for a vulnerability: the highest
// CVSS v3 base score across its severity vectors, falling back to any named
// severity the database supplies (GHSA advisories carry one directly).
func severityOf(v *osvVuln) string {
	best := ""
	bestScore := -1.0
	for _, s := range v.Severity {
		if !strings.HasPrefix(strings.ToUpper(s.Type), "CVSS_V3") {
			continue
		}
		score, ok := cvssV3BaseScore(s.Score)
		if ok && score > bestScore {
			bestScore = score
			best = bucketFromScore(score)
		}
	}
	if best != "" {
		return best
	}
	// No usable CVSS vector: fall back to a named label.
	if lbl := normalizeSeverityWord(v.DatabaseSpecific.Severity); lbl != "" {
		return lbl
	}
	for _, a := range v.Affected {
		if lbl := normalizeSeverityWord(a.DatabaseSpecific.Severity); lbl != "" {
			return lbl
		}
	}
	return SeverityUnknown
}

func bucketFromScore(score float64) string {
	switch {
	case score >= 9.0:
		return SeverityCritical
	case score >= 7.0:
		return SeverityHigh
	case score >= 4.0:
		return SeverityMedium
	case score > 0.0:
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

func normalizeSeverityWord(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return SeverityCritical
	case "high", "important":
		return SeverityHigh
	case "moderate", "medium":
		return SeverityMedium
	case "low", "minor":
		return SeverityLow
	default:
		return ""
	}
}

// cvssV3BaseScore computes the CVSS v3.0/v3.1 base score from a vector string
// (e.g. "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"). It returns false when the
// vector is missing a required base metric. The formula follows the CVSS v3.1
// specification.
func cvssV3BaseScore(vector string) (float64, bool) {
	m := map[string]string{}
	for part := range strings.SplitSeq(vector, "/") {
		k, val, ok := strings.Cut(part, ":")
		if ok {
			m[strings.ToUpper(k)] = strings.ToUpper(val)
		}
	}
	av, ok1 := cvssWeight(cvssAV, m["AV"])
	ac, ok2 := cvssWeight(cvssAC, m["AC"])
	ui, ok3 := cvssWeight(cvssUI, m["UI"])
	c, ok4 := cvssWeight(cvssCIA, m["C"])
	i, ok5 := cvssWeight(cvssCIA, m["I"])
	a, ok6 := cvssWeight(cvssCIA, m["A"])
	scope, ok7 := m["S"]
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
		return 0, false
	}
	changed := scope == "C"
	prTable := cvssPRUnchanged
	if changed {
		prTable = cvssPRChanged
	}
	pr, ok8 := cvssWeight(prTable, m["PR"])
	if !ok8 {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0, true
	}
	exploitability := 8.22 * av * ac * pr * ui
	var base float64
	if changed {
		base = roundUp(math.Min(1.08*(impact+exploitability), 10))
	} else {
		base = roundUp(math.Min(impact+exploitability, 10))
	}
	return base, true
}

func cvssWeight(table map[string]float64, key string) (float64, bool) {
	v, ok := table[key]
	return v, ok
}

// roundUp rounds up to one decimal place per the CVSS v3.1 Roundup definition.
func roundUp(x float64) float64 {
	return math.Ceil(x*10) / 10
}

var (
	cvssAV          = map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}
	cvssAC          = map[string]float64{"L": 0.77, "H": 0.44}
	cvssUI          = map[string]float64{"N": 0.85, "R": 0.62}
	cvssCIA         = map[string]float64{"H": 0.56, "L": 0.22, "N": 0.0}
	cvssPRUnchanged = map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	cvssPRChanged   = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
)

// preferredID returns the vulnerability identifier to display: a CVE alias when
// present (people search by CVE), else the OSV/advisory ID.
func preferredID(v *osvVuln) string {
	for _, a := range v.Aliases {
		if strings.HasPrefix(a, "CVE-") {
			return a
		}
	}
	return v.ID
}

// referenceURL picks a human-facing link for the vulnerability: an advisory
// reference when available, else the canonical OSV page.
func referenceURL(v *osvVuln) string {
	for _, r := range v.References {
		if strings.EqualFold(r.Type, "ADVISORY") && r.URL != "" {
			return r.URL
		}
	}
	for _, r := range v.References {
		if r.URL != "" {
			return r.URL
		}
	}
	return "https://osv.dev/vulnerability/" + v.ID
}

// fixedVersionFor finds the version that resolves a vulnerability for a given
// component: the last "fixed" event in a matching affected range. Matching is by
// ecosystem+name, falling back to any affected entry when the component's
// coordinates do not line up (SBOM naming varies).
func fixedVersionFor(v *osvVuln, ecosystem, name string) string {
	name = strings.ToLower(name)
	if fixed := scanAffectedFixed(v, ecosystem, name, true); fixed != "" {
		return fixed
	}
	return scanAffectedFixed(v, ecosystem, name, false)
}

func scanAffectedFixed(v *osvVuln, ecosystem, name string, strict bool) string {
	for _, aff := range v.Affected {
		if strict {
			if !strings.EqualFold(aff.Package.Ecosystem, ecosystem) || !strings.EqualFold(aff.Package.Name, name) {
				continue
			}
		}
		for _, rng := range aff.Ranges {
			last := ""
			for _, ev := range rng.Events {
				if ev.Fixed != "" {
					last = ev.Fixed
				}
			}
			if last != "" {
				return last
			}
		}
	}
	return ""
}
