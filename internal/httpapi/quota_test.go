package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestProjectQuotaEnforced drives a real push through the generic adapter (which
// resolves writes through the shared repository service, where quota enforcement
// lives) and asserts that once a project is at or over its quota, further writes
// are rejected — while the write that crosses the line is allowed.
func TestProjectQuotaEnforced(t *testing.T) {
	api := newTestAPI(t)

	// A project with a 100-byte quota that auto-creates repos on push.
	if rr := api.post(t, "/api/v1/projects", `{"key":"q","name":"Q","allowAutoCreate":true,"quotaBytes":100}`); rr.Code != http.StatusCreated {
		t.Fatalf("create project: %d (%s)", rr.Code, rr.Body.String())
	}

	// First push is 200 bytes: usage is 0 < 100 at check time, so the crossing
	// write is allowed.
	if rr := api.do(t, http.MethodPut, "/generic/q/files/a.bin", strings.Repeat("x", 200), true); rr.Code != http.StatusCreated {
		t.Fatalf("first push: %d (%s)", rr.Code, rr.Body.String())
	}

	// Second push: usage (200) is now over quota (100), so it is rejected.
	rr := api.do(t, http.MethodPut, "/generic/q/files/b.bin", "y", true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("over-quota push: %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "quota") {
		t.Errorf("expected a quota message, got: %s", rr.Body.String())
	}
}

// TestQuotaUsageEndpoints exercises reading usage and setting the quota, and that
// usage reflects a real push.
func TestQuotaUsageEndpoints(t *testing.T) {
	api := newTestAPI(t)
	if rr := api.post(t, "/api/v1/projects", `{"key":"s","name":"S","allowAutoCreate":true}`); rr.Code != http.StatusCreated {
		t.Fatalf("create project: %d", rr.Code)
	}

	usage := func() (quota, used int64) {
		rr := api.do(t, http.MethodGet, "/api/v1/projects/s/usage", "", true)
		if rr.Code != http.StatusOK {
			t.Fatalf("usage: %d (%s)", rr.Code, rr.Body.String())
		}
		var body struct {
			QuotaBytes int64 `json:"quotaBytes"`
			UsedBytes  int64 `json:"usedBytes"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode usage: %v", err)
		}
		return body.QuotaBytes, body.UsedBytes
	}

	if q, u := usage(); q != 0 || u != 0 {
		t.Fatalf("fresh project usage = quota %d used %d, want 0/0", q, u)
	}

	// Set a quota and confirm it round-trips.
	if rr := api.do(t, http.MethodPut, "/api/v1/projects/s/quota", `{"quotaBytes":5000}`, true); rr.Code != http.StatusOK {
		t.Fatalf("set quota: %d (%s)", rr.Code, rr.Body.String())
	}
	if q, _ := usage(); q != 5000 {
		t.Fatalf("quota after set = %d, want 5000", q)
	}

	// A push increases reported usage.
	if rr := api.do(t, http.MethodPut, "/generic/s/files/x.bin", strings.Repeat("x", 1234), true); rr.Code != http.StatusCreated {
		t.Fatalf("push: %d", rr.Code)
	}
	if _, u := usage(); u < 1234 {
		t.Errorf("usage after push = %d, want >= 1234", u)
	}
}

// TestUnlimitedQuotaAllowsWrites confirms the default (quota 0) never blocks.
func TestUnlimitedQuotaAllowsWrites(t *testing.T) {
	api := newTestAPI(t)
	if rr := api.post(t, "/api/v1/projects", `{"key":"u","name":"U","allowAutoCreate":true}`); rr.Code != http.StatusCreated {
		t.Fatalf("create project: %d", rr.Code)
	}
	for _, name := range []string{"a", "b", "c"} {
		if rr := api.do(t, http.MethodPut, "/generic/u/files/"+name, strings.Repeat("z", 10_000), true); rr.Code != http.StatusCreated {
			t.Fatalf("push %s under unlimited quota: %d", name, rr.Code)
		}
	}
}
