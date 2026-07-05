package oci

import "testing"

// TestBuildManifestViewSurfacesLayerAnnotations locks the seam signature
// verification depends on: cosign stores the signature (and, keyless, the
// certificate) in the signature manifest's layer annotations, so the browser must
// surface layer annotations, not drop them.
func TestBuildManifestViewSurfacesLayerAnnotations(t *testing.T) {
	payload := []byte(`{
		"schemaVersion":2,
		"mediaType":"application/vnd.oci.image.manifest.v1+json",
		"artifactType":"application/vnd.dev.cosign.artifact.sig.v1+json",
		"config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"sha256:cfg","size":2},
		"layers":[{
			"mediaType":"application/vnd.dev.cosign.simplesigning.v1+json",
			"digest":"sha256:payload","size":123,
			"annotations":{
				"dev.cosignproject.cosign/signature":"MEUCIQD-base64-sig",
				"dev.sigstore.cosign/certificate":"-----BEGIN CERTIFICATE-----\n..."
			}
		}]
	}`)

	view := buildManifestView("sha256:sig", "application/vnd.oci.image.manifest.v1+json", payload)
	if len(view.Layers) != 1 {
		t.Fatalf("layers = %d, want 1", len(view.Layers))
	}
	annos := view.Layers[0].Annotations
	if annos["dev.cosignproject.cosign/signature"] != "MEUCIQD-base64-sig" {
		t.Errorf("signature annotation not surfaced: %+v", annos)
	}
	if annos["dev.sigstore.cosign/certificate"] == "" {
		t.Error("certificate annotation not surfaced")
	}
}
