// Package sign verifies cosign signatures attached to OCI artifacts. It performs
// real cryptographic verification — the signature bytes are checked against a
// public key over the exact signed payload — rather than merely reporting that a
// signature is present. It depends only on the Go standard library, so it adds no
// external service and stays inside the single-binary promise.
//
// Two trust models are supported:
//   - keyless: the signature ships an X.509 certificate (Fulcio-issued in the
//     Sigstore ecosystem); the signature is verified against the certificate's
//     public key and the signer identity (SAN) and OIDC issuer are extracted.
//   - key-based: the caller supplies a trusted public key (PEM) — typically a
//     per-project verification key.
//
// Full keyless trust (chaining the certificate to a Fulcio root and proving Rekor
// transparency-log inclusion) is a further step; what is verified here is that the
// payload was signed by the certificate's key and that the payload binds to this
// image digest.
package sign

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
)

// Cosign layer-annotation keys. cosign has used two namespaces over time; both are
// accepted so signatures from older and newer clients verify.
const (
	annoSignature   = "dev.cosignproject.cosign/signature"
	annoCertificate = "dev.sigstore.cosign/certificate"
	annoCertLegacy  = "dev.cosignproject.cosign/certificate"
)

// Fulcio X.509 extension OIDs carrying the OIDC issuer of a keyless signer.
var (
	oidIssuerV1 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	oidIssuerV2 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
)

// Trust models.
const (
	KeyTypeKeyless    = "keyless"
	KeyTypeKey        = "key"
	KeyTypeUnverified = "unverified"
)

// Verification is the outcome of checking one cosign signature.
type Verification struct {
	KeyType     string // keyless | key | unverified
	Verified    bool   // the signature is cryptographically valid over the payload
	DigestMatch bool   // the payload binds to the expected image digest
	Identity    string // keyless signer identity (SAN email or URI)
	Issuer      string // keyless OIDC issuer
	KeyID       string // short fingerprint of the verifying key
	Algorithm   string // signature algorithm used
	Reason      string // why verification did not succeed, when applicable
}

// simpleSigning is the cosign "simple signing" payload: the JSON blob that is
// actually signed. Its critical.image.docker-manifest-digest binds the signature
// to a specific image manifest.
type simpleSigning struct {
	Critical struct {
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
	} `json:"critical"`
}

// VerifyCosign verifies a single cosign signature. payload is the signed
// simple-signing blob (the signature layer's content); annotations are that
// layer's annotations (carrying the signature and, for keyless, the certificate);
// trustedKeyPEM is an optional configured public key used when the signature is
// not keyless; subjectDigest is the image manifest digest the signature should
// bind to.
func VerifyCosign(payload []byte, annotations map[string]string, trustedKeyPEM, subjectDigest string) Verification {
	v := Verification{KeyType: KeyTypeUnverified}
	v.DigestMatch = digestBinds(payload, subjectDigest)

	sigB64 := strings.TrimSpace(annotations[annoSignature])
	if sigB64 == "" {
		v.Reason = "no signature annotation on the referrer"
		return v
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		v.Reason = "signature is not valid base64"
		return v
	}

	certPEM := firstNonEmpty(annotations[annoCertificate], annotations[annoCertLegacy])
	switch {
	case certPEM != "":
		return verifyWithCertificate(payload, sig, certPEM, v)
	case strings.TrimSpace(trustedKeyPEM) != "":
		return verifyWithKey(payload, sig, trustedKeyPEM, v)
	default:
		v.Reason = "signature is not keyless and no verification key is configured"
		return v
	}
}

func verifyWithCertificate(payload, sig []byte, certPEM string, v Verification) Verification {
	v.KeyType = KeyTypeKeyless
	cert, err := parseFirstCertificate(certPEM)
	if err != nil {
		v.Reason = "signing certificate could not be parsed: " + err.Error()
		return v
	}
	v.Identity = certIdentity(cert)
	v.Issuer = certIssuer(cert)
	v.KeyID = keyFingerprint(cert.PublicKey)
	ok, algo := verifySignature(cert.PublicKey, payload, sig)
	v.Algorithm = algo
	v.Verified = ok
	if !ok {
		v.Reason = "signature does not verify against the certificate key"
	}
	return v
}

func verifyWithKey(payload, sig []byte, keyPEM string, v Verification) Verification {
	v.KeyType = KeyTypeKey
	pub, err := parsePublicKey(keyPEM)
	if err != nil {
		v.Reason = "verification key could not be parsed: " + err.Error()
		return v
	}
	v.KeyID = keyFingerprint(pub)
	ok, algo := verifySignature(pub, payload, sig)
	v.Algorithm = algo
	v.Verified = ok
	if !ok {
		v.Reason = "signature does not verify against the configured key"
	}
	return v
}

// verifySignature checks sig over payload for the supported cosign key types.
// ECDSA and RSA sign the SHA-256 of the payload; Ed25519 signs the payload
// directly.
func verifySignature(pub crypto.PublicKey, payload, sig []byte) (bool, string) {
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		h := sha256.Sum256(payload)
		return ecdsa.VerifyASN1(key, h[:], sig), "ECDSA-" + key.Curve.Params().Name
	case *rsa.PublicKey:
		h := sha256.Sum256(payload)
		return rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], sig) == nil, "RSA"
	case ed25519.PublicKey:
		return ed25519.Verify(key, payload, sig), "Ed25519"
	default:
		return false, "unsupported"
	}
}

// digestBinds reports whether the simple-signing payload names the expected image
// digest, so a valid signature for one image cannot be replayed onto another.
func digestBinds(payload []byte, subjectDigest string) bool {
	if subjectDigest == "" {
		return false
	}
	var doc simpleSigning
	if err := json.Unmarshal(payload, &doc); err != nil {
		return false
	}
	return doc.Critical.Image.DockerManifestDigest == subjectDigest
}

func parseFirstCertificate(pemData string) (*x509.Certificate, error) {
	rest := []byte(pemData)
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("no PEM certificate found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		rest = remaining
	}
}

func parsePublicKey(pemData string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("no PEM public key found")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

// certIdentity returns the signer's subject-alternative-name identity: an email
// (OIDC email identity) or, failing that, a URI (SPIFFE / CI workflow identity).
func certIdentity(cert *x509.Certificate) string {
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}
	for _, u := range cert.URIs {
		if u != nil {
			return u.String()
		}
	}
	return ""
}

// certIssuer extracts the OIDC issuer from the Fulcio extension, preferring the
// newer DER-encoded form.
func certIssuer(cert *x509.Certificate) string {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidIssuerV2) {
			var s string
			if _, err := asn1.Unmarshal(ext.Value, &s); err == nil && s != "" {
				return s
			}
		}
	}
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidIssuerV1) {
			return string(ext.Value)
		}
	}
	return ""
}

// keyFingerprint is a short, stable identifier for a public key: the first bytes
// of the SHA-256 of its PKIX encoding.
func keyFingerprint(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:8])
}

// LayerHasSignature reports whether a layer's annotations carry a cosign
// signature, so the caller can pick the signature layer out of a signature
// manifest.
func LayerHasSignature(annotations map[string]string) bool {
	return strings.TrimSpace(annotations[annoSignature]) != ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
