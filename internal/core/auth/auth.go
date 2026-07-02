// Package auth owns identity: users, password verification, and cookie
// sessions. It is the single place authentication decisions are made; HTTP
// handlers and adapters consume it, they never re-implement it.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// SessionTTL is how long a login stays valid before re-authentication.
const SessionTTL = 7 * 24 * time.Hour

// sessionTokenBytes is the entropy behind a session cookie (256 bits).
const sessionTokenBytes = 32

var (
	// ErrInvalidCredentials is returned for both unknown users and bad
	// passwords, so callers cannot probe which usernames exist.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrNoSession means the presented cookie matched no live session.
	ErrNoSession = errors.New("no valid session")
)

// User is the domain view of an account.
type User struct {
	ID        string
	Username  string
	Email     string
	IsAdmin   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Service provides authentication operations backed by the metadata store.
type Service struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewService wires the service to an open database.
func NewService(sqlDB *sql.DB) *Service {
	return &Service{
		db:  sqlDB,
		q:   db.New(sqlDB),
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Authenticate verifies a username/password pair and returns the user. It runs
// bcrypt even when the user is unknown, so response timing does not reveal
// whether a username exists.
func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	row, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Compare against a dummy hash to equalize timing, then fail.
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("looking up user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return toUser(row), nil
}

// StartSession issues a new session for a user and returns the raw cookie token
// (shown once) alongside its expiry. Only a hash of the token is stored.
func (s *Service) StartSession(ctx context.Context, userID string) (token string, expiresAt time.Time, err error) {
	token, err = newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expiresAt = now.Add(SessionTTL)

	if _, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        id.New("sess"),
		TokenHash: hashToken(token),
		UserID:    userID,
		CreatedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("creating session: %w", err)
	}
	return token, expiresAt, nil
}

// ResolveSession returns the user for a raw session token, or ErrNoSession if
// the token is unknown or expired. Expired sessions are best-effort deleted.
func (s *Service) ResolveSession(ctx context.Context, token string) (User, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNoSession
		}
		return User{}, fmt.Errorf("resolving session: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, row.Session.ExpiresAt)
	if err != nil {
		return User{}, fmt.Errorf("parsing session expiry: %w", err)
	}
	if s.now().After(expiresAt) {
		_ = s.q.DeleteSessionByTokenHash(ctx, row.Session.TokenHash)
		return User{}, ErrNoSession
	}
	return toUser(row.User), nil
}

// EndSession revokes the session identified by the raw token. Revoking an
// unknown token is not an error (logout is idempotent).
func (s *Service) EndSession(ctx context.Context, token string) error {
	if err := s.q.DeleteSessionByTokenHash(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("ending session: %w", err)
	}
	return nil
}

// toUser converts a stored row to the domain type. Timestamp parse errors are
// swallowed to zero values because these fields are informational; identity
// (ID, username, admin flag) is what callers gate on.
func toUser(row db.User) User {
	created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return User{
		ID:        row.ID,
		Username:  row.Username,
		Email:     row.Email,
		IsAdmin:   row.IsAdmin != 0,
		CreatedAt: created,
		UpdatedAt: updated,
	}
}

func newToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dummyHash is a valid bcrypt hash of a throwaway password, used to keep
// Authenticate's timing constant for unknown users.
var dummyHash = mustHash("platbor-timing-equalizer")

func mustHash(password string) []byte {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("auth: hashing dummy password: %v", err))
	}
	return h
}
