package scan

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCVSSV3BaseScore(t *testing.T) {
	cases := []struct {
		vector string
		want   float64
		bucket string
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, SeverityCritical},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10.0, SeverityCritical},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:L/I:N/A:N", 3.1, SeverityLow},
		{"CVSS:3.0/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 7.8, SeverityHigh},
	}
	for _, c := range cases {
		got, ok := cvssV3BaseScore(c.vector)
		if !ok {
			t.Fatalf("%s: not parsed", c.vector)
		}
		if math.Abs(got-c.want) > 0.05 {
			t.Errorf("%s: score = %.1f, want %.1f", c.vector, got, c.want)
		}
		if b := bucketFromScore(got); b != c.bucket {
			t.Errorf("%s: bucket = %s, want %s", c.vector, b, c.bucket)
		}
	}
}

func TestCVSSV3BaseScoreRejectsIncomplete(t *testing.T) {
	if _, ok := cvssV3BaseScore("CVSS:3.1/AV:N/AC:L"); ok {
		t.Fatal("expected incomplete vector to be rejected")
	}
}

func TestEcosystemFromPURL(t *testing.T) {
	cases := map[string]string{
		"pkg:npm/left-pad@1.0.0":         "npm",
		"pkg:golang/rsc.io/quote@v1.5.2": "Go",
		"pkg:pypi/requests@2.0":          "PyPI",
		"pkg:cargo/cfg-if@1.0.0":         "crates.io",
		"pkg:maven/org.example/lib@1.0":  "Maven",
		"pkg:apk/alpine/openssl@3.0.0":   "Alpine",
		"pkg:unknownecosystem/thing@1":   "unknownecosystem",
	}
	for purl, want := range cases {
		if got := ecosystemFromPURL(purl); got != want {
			t.Errorf("ecosystemFromPURL(%q) = %q, want %q", purl, got, want)
		}
	}
}

func TestSeverityWordFallback(t *testing.T) {
	v := &osvVuln{}
	v.DatabaseSpecific.Severity = "HIGH"
	if got := severityOf(v); got != SeverityHigh {
		t.Errorf("severityOf named = %q, want high", got)
	}
}

// TestScanEndToEnd drives a full scan against a stub OSV server: a batch query
// resolves a component to a vulnerability, which is then hydrated for its
// severity, fixed version, preferred CVE id, and advisory URL.
func TestScanEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/querybatch":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "pkg:npm/left-pad@1.0.0") {
				t.Errorf("querybatch missing purl: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{"vulns": []any{map[string]any{"id": "GHSA-abcd-1234"}}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/vulns/GHSA-abcd-1234":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "GHSA-abcd-1234",
				"summary": "left-pad is vulnerable",
				"aliases": []string{"CVE-2020-1234"},
				"severity": []any{
					map[string]any{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
				},
				"affected": []any{
					map[string]any{
						"package": map[string]any{"ecosystem": "npm", "name": "left-pad"},
						"ranges": []any{map[string]any{
							"type":   "SEMVER",
							"events": []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": "1.3.0"}},
						}},
					},
				},
				"references": []any{
					map[string]any{"type": "ADVISORY", "url": "https://example.test/advisory"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	scanner := NewScanner(srv.URL)
	res, err := scanner.Scan(context.Background(), []Component{
		{Name: "left-pad", Version: "1.0.0", PURL: "pkg:npm/left-pad@1.0.0"},
		{Name: "safe-pkg", Version: "2.0.0", PURL: "pkg:npm/safe-pkg@2.0.0"}, // no vuln (index 1, no result)
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.ComponentCount != 2 {
		t.Errorf("ComponentCount = %d, want 2", res.ComponentCount)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(res.Findings))
	}
	f := res.Findings[0]
	if f.VulnID != "CVE-2020-1234" {
		t.Errorf("VulnID = %q, want CVE-2020-1234 (alias preferred)", f.VulnID)
	}
	if f.Severity != SeverityCritical {
		t.Errorf("Severity = %q, want critical", f.Severity)
	}
	if f.FixedVersion != "1.3.0" {
		t.Errorf("FixedVersion = %q, want 1.3.0", f.FixedVersion)
	}
	if f.ReferenceURL != "https://example.test/advisory" {
		t.Errorf("ReferenceURL = %q", f.ReferenceURL)
	}
	if f.Ecosystem != "npm" {
		t.Errorf("Ecosystem = %q, want npm", f.Ecosystem)
	}
	if res.Counts[SeverityCritical] != 1 {
		t.Errorf("critical count = %d, want 1", res.Counts[SeverityCritical])
	}
}

func TestScanSkipsComponentsWithoutPURL(t *testing.T) {
	// No purl anywhere -> OSV is never contacted (endpoint points nowhere).
	scanner := NewScanner("http://127.0.0.1:0")
	res, err := scanner.Scan(context.Background(), []Component{{Name: "x", Version: "1"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.ComponentCount != 0 || len(res.Findings) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}
