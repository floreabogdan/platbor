package id

import (
	"strings"
	"testing"
)

func TestNewHasPrefixAndIsUnique(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for range n {
		got := New("proj")
		if !strings.HasPrefix(got, "proj_") {
			t.Fatalf("id %q missing prefix", got)
		}
		if seen[got] {
			t.Fatalf("duplicate id generated: %q", got)
		}
		seen[got] = true
	}
}

func TestNewSuffixIsLowercaseBase32(t *testing.T) {
	got := New("audit")
	suffix := strings.TrimPrefix(got, "audit_")
	if suffix == got {
		t.Fatalf("prefix not applied: %q", got)
	}
	for _, r := range suffix {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", r) {
			t.Errorf("id %q has non-base32 rune %q", got, r)
		}
	}
}
