package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// tokenPrefixText marks a Platbor personal access token and doubles as the
// stored display prefix boundary.
const tokenPrefixText = "pbt_"

// tokenRandomBytes is the entropy after the prefix (192 bits).
const tokenRandomBytes = 24

// displayPrefixLen is how much of the raw token is kept in cleartext for
// recognition in a list (the prefix plus a few characters).
const displayPrefixLen = len(tokenPrefixText) + 8

var (
	// ErrInvalidToken means the presented token is unknown or expired.
	ErrInvalidToken = errors.New("invalid or expired token")
	// ErrTokenNotFound means no token with that id belongs to the user.
	ErrTokenNotFound = errors.New("token not found")
)

// Token is the metadata view of a personal access token. The secret itself is
// never part of this type after creation.
type Token struct {
	ID        string
	Name      string
	Prefix    string
	CreatedAt time.Time
	ExpiresAt *time.Time // nil means the token never expires
}

// CreateToken issues a new personal access token for the user. ttl of 0 means
// no expiry. The raw secret is returned once and never recoverable afterward.
func (s *Service) CreateToken(ctx context.Context, userID, name string, ttl time.Duration) (raw string, tok Token, err error) {
	raw, err = newAPIToken()
	if err != nil {
		return "", Token{}, err
	}

	now := s.now()
	var expires sql.NullString
	var expiresPtr *time.Time
	if ttl > 0 {
		exp := now.Add(ttl)
		expires = sql.NullString{String: exp.Format(time.RFC3339Nano), Valid: true}
		expiresPtr = &exp
	}

	row, err := s.q.CreateAPIToken(ctx, db.CreateAPITokenParams{
		ID:        id.New("tok"),
		UserID:    userID,
		Name:      name,
		TokenHash: hashToken(raw),
		Prefix:    raw[:displayPrefixLen],
		CreatedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: expires,
	})
	if err != nil {
		return "", Token{}, fmt.Errorf("creating token: %w", err)
	}

	tok = Token{ID: row.ID, Name: row.Name, Prefix: row.Prefix, CreatedAt: now, ExpiresAt: expiresPtr}
	return raw, tok, nil
}

// ListTokens returns the user's tokens, newest first.
func (s *Service) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := s.q.ListAPITokensByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	tokens := make([]Token, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, toToken(row))
	}
	return tokens, nil
}

// DeleteToken revokes a token the user owns. Deleting an unknown or
// not-owned token returns ErrTokenNotFound.
func (s *Service) DeleteToken(ctx context.Context, userID, tokenID string) error {
	affected, err := s.q.DeleteAPIToken(ctx, db.DeleteAPITokenParams{ID: tokenID, UserID: userID})
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	if affected == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// AuthenticateToken resolves a raw token to its owner, rejecting unknown or
// expired tokens with ErrInvalidToken.
func (s *Service) AuthenticateToken(ctx context.Context, raw string) (User, error) {
	row, err := s.q.GetAPITokenByHash(ctx, hashToken(raw))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrInvalidToken
		}
		return User{}, fmt.Errorf("resolving token: %w", err)
	}

	if row.ApiToken.ExpiresAt.Valid {
		expiresAt, err := time.Parse(time.RFC3339Nano, row.ApiToken.ExpiresAt.String)
		if err != nil {
			return User{}, fmt.Errorf("parsing token expiry: %w", err)
		}
		if s.now().After(expiresAt) {
			return User{}, ErrInvalidToken
		}
	}
	return toUser(row.User), nil
}

func toToken(row db.ApiToken) Token {
	created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var expiresPtr *time.Time
	if row.ExpiresAt.Valid {
		if exp, err := time.Parse(time.RFC3339Nano, row.ExpiresAt.String); err == nil {
			expiresPtr = &exp
		}
	}
	return Token{
		ID:        row.ID,
		Name:      row.Name,
		Prefix:    row.Prefix,
		CreatedAt: created,
		ExpiresAt: expiresPtr,
	}
}

func newAPIToken() (string, error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return tokenPrefixText + base64.RawURLEncoding.EncodeToString(buf), nil
}
