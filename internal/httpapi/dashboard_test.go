package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDashboardEndpoint(t *testing.T) {
	a := newTestAPI(t)
	// Creating a project also writes a project.create audit entry.
	if rr := a.post(t, "/api/v1/projects", `{"key":"acme","name":"Acme"}`); rr.Code != http.StatusCreated {
		t.Fatalf("seed project: status = %d", rr.Code)
	}

	rr := a.get(t, "/api/v1/dashboard")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got dashboardResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Summary.Projects != 1 {
		t.Errorf("summary.projects = %d, want 1", got.Summary.Projects)
	}
	if got.Summary.Repositories != 0 || got.Summary.Tags != 0 {
		t.Errorf("expected empty registry, got repos=%d tags=%d", got.Summary.Repositories, got.Summary.Tags)
	}

	// The project creation shows up in the activity feed, attributed and scoped.
	var found bool
	for _, e := range got.Activity {
		if e.Action == "project.create" && e.Actor == "admin" && e.ProjectKey == "acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("project.create not in activity feed: %+v", got.Activity)
	}
}

func TestDashboardRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	if rr := a.do(t, http.MethodGet, "/api/v1/dashboard", "", false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}
