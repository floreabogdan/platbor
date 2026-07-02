package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bearer issues a request authenticated with an Authorization: Bearer token
// (no session cookie), the way a machine client would.
func (a testAPI) bearer(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, req)
	return rr
}

func TestTokenCreateUseListRevoke(t *testing.T) {
	a := newTestAPI(t)

	// Create a token (browser session).
	rr := a.post(t, "/api/v1/tokens", `{"name":"ci"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var created createTokenResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding create body: %v", err)
	}
	if created.Token == "" || created.ID == "" || created.Name != "ci" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	// The raw token authenticates a machine request with no cookie.
	if rr := a.bearer(t, http.MethodGet, "/api/v1/projects", created.Token); rr.Code != http.StatusOK {
		t.Fatalf("bearer projects: status = %d, want 200", rr.Code)
	}

	// The token lists without exposing the secret.
	listRR := a.get(t, "/api/v1/tokens")
	if listRR.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", listRR.Code)
	}
	if body := listRR.Body.String(); !strings.Contains(body, created.Prefix) || strings.Contains(body, created.Token) {
		t.Fatalf("list should show the prefix, not the secret: %s", body)
	}

	// Revoke it, after which the token is rejected.
	if rr := a.do(t, http.MethodDelete, "/api/v1/tokens/"+created.ID, "", true); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", rr.Code)
	}
	if rr := a.bearer(t, http.MethodGet, "/api/v1/projects", created.Token); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked bearer: status = %d, want 401", rr.Code)
	}
}

func TestTokenCreateValidation(t *testing.T) {
	a := newTestAPI(t)
	for _, body := range []string{
		`{"name":""}`,
		`{"name":"ok","expiresInDays":-1}`,
		`{"name":"ok","expiresInDays":99999}`,
		`not json`,
	} {
		if rr := a.post(t, "/api/v1/tokens", body); rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rr.Code)
		}
	}
}

func TestDeleteUnknownTokenReturns404(t *testing.T) {
	a := newTestAPI(t)
	if rr := a.do(t, http.MethodDelete, "/api/v1/tokens/tok_missing", "", true); rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestInvalidBearerIsUnauthorized(t *testing.T) {
	a := newTestAPI(t)
	if rr := a.bearer(t, http.MethodGet, "/api/v1/projects", "pbt_not_real"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}
