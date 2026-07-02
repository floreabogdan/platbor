package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// algoSHA256 is the only digest algorithm Platbor stores under today. The
// digest format ("sha256:<hex>") matches the OCI spec so registry clients and
// the store speak the same language.
const algoSHA256 = "sha256"

// sha256Pattern matches a canonical sha256 digest: lowercase 64-hex.
var sha256Pattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// ValidateDigest reports whether d is a well-formed, supported digest.
func ValidateDigest(d string) error {
	if !sha256Pattern.MatchString(d) {
		return fmt.Errorf("%w: %q", ErrInvalidDigest, d)
	}
	return nil
}

// DigestBytes returns the canonical sha256 digest of data.
func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return algoSHA256 + ":" + hex.EncodeToString(sum[:])
}

// digestReader streams r through sha256, returning the canonical digest and the
// number of bytes read.
func digestReader(r io.Reader) (digest string, size int64, err error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("hashing content: %w", err)
	}
	return algoSHA256 + ":" + hex.EncodeToString(h.Sum(nil)), n, nil
}

// digestHex returns the hex portion of a validated digest (the part after
// "sha256:"), used to build storage paths.
func digestHex(d string) string {
	_, hexPart, _ := strings.Cut(d, ":")
	return hexPart
}
