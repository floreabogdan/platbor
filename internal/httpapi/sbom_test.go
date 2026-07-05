package httpapi

import "testing"

func TestParseCycloneDX(t *testing.T) {
	data := []byte(`{
		"bomFormat":"CycloneDX","specVersion":"1.5",
		"components":[
			{"type":"library","name":"left-pad","version":"1.3.0","purl":"pkg:npm/left-pad@1.3.0","licenses":[{"license":{"id":"WTFPL"}}]},
			{"type":"library","name":"react","version":"18.2.0","licenses":[{"license":{"name":"MIT License"}}]},
			{"type":"application","name":"react","version":"18.2.0"}
		]}`)
	got, err := parseSBOM(data)
	if err != nil {
		t.Fatalf("parseSBOM: %v", err)
	}
	if got.Format != "cyclonedx" {
		t.Fatalf("format = %q, want cyclonedx", got.Format)
	}
	// Duplicate (react,18.2.0) collapses to one; sorted by name then version.
	if len(got.Components) != 2 {
		t.Fatalf("components = %d, want 2: %+v", len(got.Components), got.Components)
	}
	if got.Components[0].Name != "left-pad" || got.Components[0].License != "WTFPL" {
		t.Errorf("first component = %+v", got.Components[0])
	}
	if got.Components[0].PURL != "pkg:npm/left-pad@1.3.0" {
		t.Errorf("purl = %q, want pkg:npm/left-pad@1.3.0", got.Components[0].PURL)
	}
	if got.Components[1].Name != "react" || got.Components[1].License != "MIT License" {
		t.Errorf("second component = %+v", got.Components[1])
	}
}

func TestParseSPDX(t *testing.T) {
	data := []byte(`{
		"spdxVersion":"SPDX-2.3","name":"image",
		"packages":[
			{"name":"openssl","versionInfo":"3.0.2","licenseConcluded":"Apache-2.0","externalRefs":[{"referenceCategory":"PACKAGE-MANAGER","referenceType":"purl","referenceLocator":"pkg:generic/openssl@3.0.2"}]},
			{"name":"zlib","versionInfo":"1.2.11","licenseConcluded":"NOASSERTION","licenseDeclared":"Zlib"}
		]}`)
	got, err := parseSBOM(data)
	if err != nil {
		t.Fatalf("parseSBOM: %v", err)
	}
	if got.Format != "spdx" || len(got.Components) != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
	// NOASSERTION falls back to the declared license.
	byName := map[string]sbomComponent{}
	for _, c := range got.Components {
		byName[c.Name] = c
	}
	if byName["openssl"].License != "Apache-2.0" || byName["zlib"].License != "Zlib" {
		t.Errorf("licenses = %+v", byName)
	}
	if byName["openssl"].PURL != "pkg:generic/openssl@3.0.2" {
		t.Errorf("openssl purl = %q", byName["openssl"].PURL)
	}
}

func TestParseSBOMUnrecognized(t *testing.T) {
	if _, err := parseSBOM([]byte(`{"hello":"world"}`)); err == nil {
		t.Error("expected an error for a non-SBOM document")
	}
	if _, err := parseSBOM([]byte(`not json`)); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}
