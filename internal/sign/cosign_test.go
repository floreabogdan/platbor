package sign

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
)

const testDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func signingPayload(t *testing.T, digest string) []byte {
	t.Helper()
	doc := map[string]any{
		"critical": map[string]any{
			"image": map[string]any{"docker-manifest-digest": digest},
			"type":  "cosign container image signature",
		},
		"optional": nil,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func ecdsaSign(t *testing.T, key *ecdsa.PrivateKey, payload []byte) string {
	t.Helper()
	h := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, h[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func pubKeyPEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestVerifyWithKey(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := signingPayload(t, testDigest)
	annos := map[string]string{annoSignature: ecdsaSign(t, key, payload)}

	v := VerifyCosign(payload, annos, pubKeyPEM(t, key), testDigest)
	if !v.Verified {
		t.Fatalf("expected verified, reason: %s", v.Reason)
	}
	if v.KeyType != KeyTypeKey {
		t.Errorf("KeyType = %q, want key", v.KeyType)
	}
	if !v.DigestMatch {
		t.Error("expected digest to bind")
	}
	if v.KeyID == "" {
		t.Error("expected a key fingerprint")
	}
}

func TestVerifyTamperedPayloadFails(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := signingPayload(t, testDigest)
	annos := map[string]string{annoSignature: ecdsaSign(t, key, payload)}

	tampered := signingPayload(t, testDigest)
	tampered[0] ^= 0xff // corrupt the signed bytes
	v := VerifyCosign(tampered, annos, pubKeyPEM(t, key), testDigest)
	if v.Verified {
		t.Fatal("expected verification to fail on a tampered payload")
	}
	if v.Reason == "" {
		t.Error("expected a failure reason")
	}
}

func TestVerifyDigestMismatch(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := signingPayload(t, testDigest)
	annos := map[string]string{annoSignature: ecdsaSign(t, key, payload)}

	// A cryptographically valid signature, but for a different image digest.
	v := VerifyCosign(payload, annos, pubKeyPEM(t, key), "sha256:deadbeef")
	if !v.Verified {
		t.Fatal("signature itself should verify")
	}
	if v.DigestMatch {
		t.Error("expected digest binding to fail for a different subject")
	}
}

func TestVerifyNoKeyNoCert(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := signingPayload(t, testDigest)
	annos := map[string]string{annoSignature: ecdsaSign(t, key, payload)}

	v := VerifyCosign(payload, annos, "", testDigest)
	if v.Verified || v.KeyType != KeyTypeUnverified {
		t.Errorf("expected unverified without a key/cert, got %+v", v)
	}
	// Digest binding is still computed from the payload alone.
	if !v.DigestMatch {
		t.Error("digest binding should still be checked")
	}
}

func TestVerifyKeyless(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	payload := signingPayload(t, testDigest)

	issuer := "https://token.actions.githubusercontent.com"
	issuerDER, err := asn1.Marshal(issuer)
	if err != nil {
		t.Fatalf("marshal issuer: %v", err)
	}
	uri, _ := url.Parse("https://github.com/acme/repo/.github/workflows/release.yml@refs/heads/main")
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(1),
		Subject:        pkix.Name{CommonName: "sigstore"},
		EmailAddresses: []string{"ci@acme.example"},
		URIs:           []*url.URL{uri},
		ExtraExtensions: []pkix.Extension{
			{Id: oidIssuerV2, Value: issuerDER},
		},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	annos := map[string]string{
		annoSignature:   ecdsaSign(t, key, payload),
		annoCertificate: certPEM,
	}
	v := VerifyCosign(payload, annos, "", testDigest)
	if !v.Verified {
		t.Fatalf("expected keyless verification, reason: %s", v.Reason)
	}
	if v.KeyType != KeyTypeKeyless {
		t.Errorf("KeyType = %q, want keyless", v.KeyType)
	}
	if v.Identity != "ci@acme.example" {
		t.Errorf("Identity = %q, want ci@acme.example", v.Identity)
	}
	if v.Issuer != issuer {
		t.Errorf("Issuer = %q, want %q", v.Issuer, issuer)
	}
}
