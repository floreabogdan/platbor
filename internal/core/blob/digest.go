package blob

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"regexp"
	"strings"
)

// Supported digest algorithms. sha256 is the OCI-required default and the one
// Platbor uses for content it hashes itself (manifests); sha512 is optional in
// the spec but stored too, so clients that pin sha512 digests interoperate.
const (
	algoSHA256 = "sha256"
	algoSHA512 = "sha512"
)

// supportedAlgos is the enumeration order Walk uses to sweep the store; keeping
// it in one place means adding an algorithm touches nothing else.
var supportedAlgos = []string{algoSHA256, algoSHA512}

// digestPattern matches a canonical digest for a supported algorithm: lowercase
// hex of that algorithm's width (sha256 = 64, sha512 = 128).
var digestPattern = regexp.MustCompile(`^(sha256:[a-f0-9]{64}|sha512:[a-f0-9]{128})$`)

// ValidateDigest reports whether d is a well-formed, supported digest.
func ValidateDigest(d string) error {
	if !digestPattern.MatchString(d) {
		return fmt.Errorf("%w: %q", ErrInvalidDigest, d)
	}
	return nil
}

// DigestBytes returns the canonical sha256 digest of data. It is used for
// content Platbor hashes itself (e.g. a manifest keyed by tag), where sha256 is
// the canonical choice.
func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return algoSHA256 + ":" + hex.EncodeToString(sum[:])
}

// MatchesDigest reports whether data hashes to ref, computed in ref's own
// algorithm. It lets a caller verify a client-supplied digest (sha256 or sha512)
// without caring which algorithm was used.
func MatchesDigest(ref string, data []byte) bool {
	h, ok := newHasher(digestAlgo(ref))
	if !ok {
		return false
	}
	_, _ = h.Write(data)
	return ref == digestAlgo(ref)+":"+hex.EncodeToString(h.Sum(nil))
}

// newHasher returns a fresh hash for a digest algorithm, or false if the
// algorithm is not one Platbor supports.
func newHasher(algo string) (hash.Hash, bool) {
	switch algo {
	case algoSHA256:
		return sha256.New(), true
	case algoSHA512:
		return sha512.New(), true
	default:
		return nil, false
	}
}

// digestReader streams r through algo's hash, returning the canonical digest and
// the number of bytes read. The algorithm comes from the expected digest, so a
// commit verifies against whatever the client pushed.
func digestReader(algo string, r io.Reader) (digest string, size int64, err error) {
	h, ok := newHasher(algo)
	if !ok {
		return "", 0, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidDigest, algo)
	}
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("hashing content: %w", err)
	}
	return algo + ":" + hex.EncodeToString(h.Sum(nil)), n, nil
}

// digestAlgo returns the algorithm portion of a digest string (the part before
// the colon), e.g. "sha256".
func digestAlgo(d string) string {
	algo, _, _ := strings.Cut(d, ":")
	return algo
}

// digestHex returns the hex portion of a validated digest (the part after the
// "<algo>:" prefix), used to build storage paths.
func digestHex(d string) string {
	_, hexPart, _ := strings.Cut(d, ":")
	return hexPart
}
