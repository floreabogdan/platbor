package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// ErrDuplicateUser means the username is already taken.
var ErrDuplicateUser = errors.New("username already exists")

// CreateUser creates a local account with a bcrypt-hashed password. isAdmin
// grants instance-admin privilege (full access, bypassing project roles). This
// backs first-run bootstrap and project member management (adding a user who is
// not yet an account is out of scope; they must exist first).
func (s *Service) CreateUser(ctx context.Context, username, email, password string, isAdmin bool) (User, error) {
	if username == "" || password == "" {
		return User{}, fmt.Errorf("username and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hashing password: %w", err)
	}
	now := s.now().Format(time.RFC3339Nano)
	row, err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID:           id.New("usr"),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		IsAdmin:      boolToInt(isAdmin),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return User{}, ErrDuplicateUser
		}
		return User{}, fmt.Errorf("creating user: %w", err)
	}
	return toUser(row), nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
