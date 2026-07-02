package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// BootstrapResult reports what Bootstrap did, so the caller can tell the
// operator how to log in on first run.
type BootstrapResult struct {
	// Created is true only when a fresh admin was inserted.
	Created bool
	// Username of the admin (existing or newly created).
	Username string
	// GeneratedPassword is non-empty only when no password was supplied and one
	// was generated; it must be shown to the operator once and never stored.
	GeneratedPassword string
}

// Bootstrap ensures an instance admin exists. On an empty instance it creates
// one: the supplied username (default "admin") and password, or a generated
// password when none is provided. On a populated instance it is a no-op.
func (s *Service) Bootstrap(ctx context.Context, username, password string) (BootstrapResult, error) {
	count, err := s.q.CountUsers(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return BootstrapResult{Created: false}, nil
	}

	if username == "" {
		username = "admin"
	}
	generated := ""
	if password == "" {
		password, err = randomPassword()
		if err != nil {
			return BootstrapResult{}, err
		}
		generated = password
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("hashing admin password: %w", err)
	}

	now := s.now().Format(time.RFC3339Nano)
	if _, err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID:           id.New("usr"),
		Username:     username,
		Email:        "",
		PasswordHash: string(hash),
		IsAdmin:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("creating admin user: %w", err)
	}
	return BootstrapResult{Created: true, Username: username, GeneratedPassword: generated}, nil
}

// randomPassword returns a 128-bit URL-safe random password for first-run admin
// bootstrap.
func randomPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating admin password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
