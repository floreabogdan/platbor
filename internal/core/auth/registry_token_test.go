package auth_test

import (
	"errors"
	"testing"

	"github.com/platbor/platbor/internal/core/auth"
)

func TestRegistryTokenRoundTrip(t *testing.T) {
	svc, _ := newAuthWithProject(t)
	u := auth.User{ID: "usr_1", Username: "alice", IsAdmin: true}

	token, expiresAt, err := svc.IssueRegistryToken(u)
	if err != nil {
		t.Fatalf("IssueRegistryToken: %v", err)
	}
	if token == "" || expiresAt.IsZero() {
		t.Fatalf("empty token/expiry: %q %v", token, expiresAt)
	}

	got, err := svc.VerifyRegistryToken(token)
	if err != nil {
		t.Fatalf("VerifyRegistryToken: %v", err)
	}
	if got.ID != u.ID || got.Username != u.Username || got.IsAdmin != u.IsAdmin {
		t.Errorf("identity = %+v, want %+v", got, u)
	}
}

func TestRegistryTokenRejectsTamperingAndForeignSecret(t *testing.T) {
	svc, _ := newAuthWithProject(t)
	other, _ := newAuthWithProject(t) // a different process → a different secret
	u := auth.User{ID: "usr_1", Username: "alice"}

	token, _, err := svc.IssueRegistryToken(u)
	if err != nil {
		t.Fatalf("IssueRegistryToken: %v", err)
	}

	// A token signed by another service's secret is not accepted here.
	if _, err := other.VerifyRegistryToken(token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("foreign secret: got %v, want ErrInvalidToken", err)
	}
	// Tampering with the payload invalidates the signature.
	tampered := token[:len(token)-2] + "xy"
	if _, err := svc.VerifyRegistryToken(tampered); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("tampered token: got %v, want ErrInvalidToken", err)
	}
	// A structurally wrong token is rejected, not panicked on.
	if _, err := svc.VerifyRegistryToken("garbage"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("garbage token: got %v, want ErrInvalidToken", err)
	}
}
