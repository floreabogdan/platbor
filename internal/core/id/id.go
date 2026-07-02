// Package id generates opaque, URL-safe, sortable-enough identifiers for
// externally-visible entities (projects, audit entries, ...). IDs are prefixed
// with a short type tag for debuggability: "proj_ck7f3n2p9q...".
package id

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
)

// entropyBytes is 128 bits of randomness — collision-safe for any scale Platbor
// will see, while keeping the encoded suffix compact.
const entropyBytes = 16

// lowerBase32 is Crockford-ish: lowercase, no padding, no ambiguous separators.
// We use the standard RFC 4648 alphabet lowercased; the exact alphabet is an
// implementation detail since IDs are opaque to clients.
var lowerBase32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// New returns a fresh identifier with the given type prefix, e.g.
// New("proj") -> "proj_mfrggzdfmztwq2lk". It panics only if the system CSPRNG
// fails, which indicates a broken platform rather than a recoverable condition.
func New(prefix string) string {
	buf := make([]byte, entropyBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("id: reading random bytes: %v", err))
	}
	return prefix + "_" + lowerBase32.EncodeToString(buf)
}
