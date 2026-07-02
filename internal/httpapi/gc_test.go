package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

func TestGarbageCollectionAdminDryRun(t *testing.T) {
	a := newTestAPI(t)

	rr := a.do(t, http.MethodPost, "/api/v1/registry/gc?dryRun=true", "", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got gcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.DryRun {
		t.Errorf("dryRun = false, want true")
	}
	if got.Deleted != 0 {
		t.Errorf("empty store should delete nothing, got %+v", got)
	}
}

func TestGarbageCollectionForbiddenForNonAdmin(t *testing.T) {
	a := newTestAPI(t)
	member := a.memberCookie(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/registry/gc", nil)
	req.AddCookie(member)
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin GC: status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestGarbageCollectionRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	if rr := a.do(t, http.MethodPost, "/api/v1/registry/gc", "", false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

// memberCookie creates a non-admin user with a live session and returns its
// cookie, for exercising admin-gated routes.
func (a testAPI) memberCookie(t *testing.T) *http.Cookie {
	t.Helper()
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	uid := id.New("user")
	if _, err := db.New(a.db).CreateUser(ctx, db.CreateUserParams{
		ID:           uid,
		Username:     "member",
		Email:        "member@example.com",
		PasswordHash: string(hash),
		IsAdmin:      0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := a.auth.StartSession(ctx, uid)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}
}
