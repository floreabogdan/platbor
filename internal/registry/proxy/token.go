package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// negotiateToken satisfies a Bearer challenge: it parses the WWW-Authenticate
// header, requests a token from the realm (with basic credentials when the
// upstream is private), caches it, and returns it.
func (c *Client) negotiateToken(ctx context.Context, up Upstream, challenge, scope string) (string, error) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("upstream 401 without a bearer realm: %q", challenge)
	}

	q := url.Values{}
	if service := params["service"]; service != "" {
		q.Set("service", service)
	}
	// Prefer the scope the upstream asked for; fall back to the pull scope we
	// computed for the repository.
	if s := params["scope"]; s != "" {
		q.Set("scope", s)
	} else {
		q.Set("scope", scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building token request: %w", err)
	}
	if up.Username != "" || up.Password != "" {
		req.SetBasicAuth(up.Username, up.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	// Registries return the token under "token" or (OAuth2 style) "access_token".
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("token endpoint returned no token")
	}

	// Spec default when expires_in is absent or tiny.
	ttl := max(time.Duration(body.ExpiresIn)*time.Second, 60*time.Second)
	c.storeToken(up.BaseURL, scope, token, ttl)
	return token, nil
}

// parseChallenge parses the parameters of a `Bearer key="value",…` challenge
// into a map. Non-bearer schemes yield an empty map.
func parseChallenge(header string) map[string]string {
	out := map[string]string{}
	rest, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return out
	}
	for _, part := range splitParams(rest) {
		key, val, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"`)
	}
	return out
}

// splitParams splits a challenge's comma-separated parameters, ignoring commas
// that fall inside a quoted value (scopes can contain commas).
func splitParams(s string) []string {
	var parts []string
	var buf strings.Builder
	inQuotes := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			buf.WriteRune(r)
		case r == ',' && !inQuotes:
			parts = append(parts, buf.String())
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	return parts
}
