package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// This file implements the short-lived bearer token the OCI token endpoint
// issues (docker's registry auth flow). The token is a stateless, HMAC-signed
// value — no database row, no audit churn — because it is opaque to the client,
// which only stores it and replays it. The registry (this same process) both
// signs and verifies it with a per-process secret, so a token does not survive a
// restart; that is fine because it lives minutes. Identity travels in the token;
// authorization is still re-checked live against project roles on every request,
// so a role change or removal takes effect within the token's short lifetime.

// RegistryTokenTTL is how long an issued registry bearer token stays valid.
const RegistryTokenTTL = 5 * time.Minute

// registryTokenPrefix versions the token format so it can evolve.
const registryTokenPrefix = "pbr1"

// registryClaims is the token payload: enough identity to authorize a request
// without a database lookup for the account itself (membership is still queried).
type registryClaims struct {
	UID   string `json:"uid"`
	User  string `json:"usr"`
	Admin bool   `json:"adm"`
	Exp   int64  `json:"exp"`
}

// IssueRegistryToken mints a signed bearer token for a user and returns it with
// its expiry.
func (s *Service) IssueRegistryToken(user User) (token string, expiresAt time.Time, err error) {
	expiresAt = s.now().Add(RegistryTokenTTL)
	payload, err := json.Marshal(registryClaims{
		UID: user.ID, User: user.Username, Admin: user.IsAdmin, Exp: expiresAt.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	body := registryTokenPrefix + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig := s.signRegistry(body)
	return body + "." + sig, expiresAt, nil
}

// VerifyRegistryToken validates a bearer token's signature and expiry and returns
// the identity it carries, or ErrInvalidToken.
func (s *Service) VerifyRegistryToken(token string) (User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != registryTokenPrefix {
		return User{}, ErrInvalidToken
	}
	body := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(s.signRegistry(body))) {
		return User{}, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, ErrInvalidToken
	}
	var claims registryClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return User{}, ErrInvalidToken
	}
	if s.now().Unix() >= claims.Exp {
		return User{}, ErrInvalidToken
	}
	return User{ID: claims.UID, Username: claims.User, IsAdmin: claims.Admin}, nil
}

// signRegistry returns the base64url HMAC-SHA256 of body under the process secret.
func (s *Service) signRegistry(body string) string {
	mac := hmac.New(sha256.New, s.regSecret)
	_, _ = mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
