package oci

import "testing"

// TestBuildManifestViewDetectsHelmChart proves an OCI manifest whose config uses
// the Helm config media type is surfaced as a chart, while an ordinary image is
// not — this is what lets the UI label charts and offer `helm pull`.
func TestBuildManifestViewDetectsHelmChart(t *testing.T) {
	const imageManifestType = "application/vnd.oci.image.manifest.v1+json"

	chart := `{
		"config": {"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": "sha256:aaa", "size": 12},
		"layers": [{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": "sha256:bbb", "size": 3400}]
	}`
	if got := buildManifestView("sha256:d1", imageManifestType, []byte(chart)); got.Kind != KindChart {
		t.Errorf("helm chart manifest: kind = %q, want %q", got.Kind, KindChart)
	}

	image := `{
		"config": {"mediaType": "application/vnd.oci.image.config.v1+json", "digest": "sha256:ccc", "size": 20},
		"layers": [{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": "sha256:ddd", "size": 5000}]
	}`
	if got := buildManifestView("sha256:d2", imageManifestType, []byte(image)); got.Kind != KindImage {
		t.Errorf("image manifest: kind = %q, want %q", got.Kind, KindImage)
	}
}
