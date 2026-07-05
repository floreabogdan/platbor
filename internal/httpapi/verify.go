package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/sign"
)

type signatureVerification struct {
	Digest      string `json:"digest"` // the signature referrer's digest
	KeyType     string `json:"keyType"`
	Verified    bool   `json:"verified"`
	DigestMatch bool   `json:"digestMatch"`
	Identity    string `json:"identity,omitempty"`
	Issuer      string `json:"issuer,omitempty"`
	KeyID       string `json:"keyId,omitempty"`
	Algorithm   string `json:"algorithm,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type attestationInfo struct {
	Digest        string `json:"digest"`
	ArtifactType  string `json:"artifactType,omitempty"`
	PredicateType string `json:"predicateType,omitempty"`
}

type verifyResponse struct {
	KeyConfigured bool                    `json:"keyConfigured"`
	Signatures    []signatureVerification `json:"signatures"`
	Attestations  []attestationInfo       `json:"attestations"`
}

// verify cryptographically checks the cosign signatures attached to a manifest
// and summarizes its attestations. For each signature it reports whether the
// signature is valid over the signed payload (not merely present), whether the
// payload binds to this image digest, and — for keyless signatures — the signer
// identity and OIDC issuer read from the embedded certificate.
func (h registryHandler) verify(w http.ResponseWriter, r *http.Request) {
	repo, image, ok := h.resolveScope(w, r)
	if !ok {
		return
	}
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		writeProblem(w, http.StatusBadRequest, "Missing digest", "the digest query parameter is required")
		return
	}

	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.log.Error("resolving project for verify", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	refs, err := h.browser.Referrers(r.Context(), repo.ID, image, digest)
	if err != nil {
		h.log.Error("listing referrers for verify", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	resp := verifyResponse{
		KeyConfigured: strings.TrimSpace(proj.VerificationKey) != "",
		Signatures:    []signatureVerification{},
		Attestations:  []attestationInfo{},
	}
	for _, ref := range refs {
		switch {
		case isSignatureReferrer(ref):
			resp.Signatures = append(resp.Signatures, h.verifyOne(r, repo.ID, image, ref, digest, proj.VerificationKey))
		case isAttestationReferrer(ref):
			resp.Attestations = append(resp.Attestations, h.describeAttestation(r, repo.ID, image, ref))
		}
	}
	writeJSON(w, h.log, http.StatusOK, resp)
}

// verifyOne verifies a single signature referrer: it opens the signature
// manifest, reads the signed payload blob and the signature/certificate
// annotations off its layer, and runs the cryptographic check.
func (h registryHandler) verifyOne(r *http.Request, repoID, image string, ref oci.Referrer, subject, keyPEM string) signatureVerification {
	out := signatureVerification{Digest: ref.Digest, KeyType: sign.KeyTypeUnverified}
	view, err := h.browser.Manifest(r.Context(), repoID, image, ref.Digest)
	if err != nil {
		out.Reason = "signature manifest could not be read"
		return out
	}
	layer, ok := signatureLayer(view)
	if !ok {
		out.Reason = "no signature layer on the referrer"
		return out
	}
	payload, err := h.readBlob(r, layer.Digest)
	if err != nil {
		out.Reason = "signed payload is not stored"
		return out
	}
	v := sign.VerifyCosign(payload, layer.Annotations, keyPEM, subject)
	return signatureVerification{
		Digest:      ref.Digest,
		KeyType:     v.KeyType,
		Verified:    v.Verified,
		DigestMatch: v.DigestMatch,
		Identity:    v.Identity,
		Issuer:      v.Issuer,
		KeyID:       v.KeyID,
		Algorithm:   v.Algorithm,
		Reason:      v.Reason,
	}
}

// describeAttestation reads an attestation's DSSE envelope and extracts the
// in-toto predicate type, so the UI can show *what* is attested (provenance,
// vulnerability report, ...) rather than an opaque blob.
func (h registryHandler) describeAttestation(r *http.Request, repoID, image string, ref oci.Referrer) attestationInfo {
	info := attestationInfo{Digest: ref.Digest, ArtifactType: ref.ArtifactType}
	view, err := h.browser.Manifest(r.Context(), repoID, image, ref.Digest)
	if err != nil || len(view.Layers) == 0 {
		return info
	}
	data, err := h.readBlob(r, view.Layers[0].Digest)
	if err != nil {
		return info
	}
	info.PredicateType = predicateType(data)
	return info
}

// signatureLayer finds the cosign signature layer within a signature manifest:
// the one whose annotations carry the signature, or that uses the simple-signing
// media type; failing that, the single layer.
func signatureLayer(view oci.ManifestView) (oci.LayerRef, bool) {
	for _, l := range view.Layers {
		if sign.LayerHasSignature(l.Annotations) || strings.Contains(strings.ToLower(l.MediaType), "simplesigning") {
			return l, true
		}
	}
	if len(view.Layers) == 1 {
		return view.Layers[0], true
	}
	return oci.LayerRef{}, false
}

// predicateType extracts the in-toto predicateType from a DSSE envelope (or a
// bare in-toto statement).
func predicateType(data []byte) string {
	// A DSSE envelope wraps the statement as a base64 payload.
	var env struct {
		PayloadType string `json:"payloadType"`
		Payload     string `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.Payload != "" {
		if decoded, err := base64.StdEncoding.DecodeString(env.Payload); err == nil {
			data = decoded
		}
	}
	var stmt struct {
		PredicateType string `json:"predicateType"`
	}
	if err := json.Unmarshal(data, &stmt); err == nil {
		return stmt.PredicateType
	}
	return ""
}

// (helpers below classify referrers by artifact type)

// isSignatureReferrer recognizes a cosign signature by its artifact type.
func isSignatureReferrer(ref oci.Referrer) bool {
	t := strings.ToLower(ref.ArtifactType + " " + ref.MediaType)
	if strings.Contains(t, "attestation") || strings.Contains(t, "sbom") {
		return false
	}
	return strings.Contains(t, "cosign") && strings.Contains(t, "sig")
}

// isAttestationReferrer recognizes an in-toto/DSSE attestation.
func isAttestationReferrer(ref oci.Referrer) bool {
	t := strings.ToLower(ref.ArtifactType + " " + ref.MediaType)
	return strings.Contains(t, "attestation") || strings.Contains(t, "in-toto") || strings.Contains(t, "dsse")
}
