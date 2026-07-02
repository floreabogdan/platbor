package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func sessionCookieFrom(rr *http.Response) *http.Cookie {
	for _, c := range rr.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func TestLoginSuccessSetsCookieAndReturnsUser(t *testing.T) {
	a := newTestAPI(t)

	rr := a.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"password123"}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	cookie := sessionCookieFrom(rr.Result())
	if cookie == nil || cookie.Value == "" {
		t.Fatal("expected a session cookie to be set")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}

	var got userResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Username != "admin" || !got.IsAdmin {
		t.Errorf("unexpected user: %+v", got)
	}
}

func TestLoginWrongPasswordReturns401(t *testing.T) {
	a := newTestAPI(t)
	rr := a.do(t, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"nope"}`, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestMeRequiresSession(t *testing.T) {
	a := newTestAPI(t)

	if rr := a.do(t, http.MethodGet, "/api/v1/auth/me", "", false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /me: status = %d, want 401", rr.Code)
	}
	if rr := a.get(t, "/api/v1/auth/me"); rr.Code != http.StatusOK {
		t.Fatalf("authenticated /me: status = %d, want 200", rr.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	a := newTestAPI(t)

	// The session works before logout.
	if rr := a.get(t, "/api/v1/auth/me"); rr.Code != http.StatusOK {
		t.Fatalf("pre-logout /me: status = %d, want 200", rr.Code)
	}
	if rr := a.post(t, "/api/v1/auth/logout", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204", rr.Code)
	}
	// After logout the same cookie is rejected.
	if rr := a.get(t, "/api/v1/auth/me"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout /me: status = %d, want 401", rr.Code)
	}
}
