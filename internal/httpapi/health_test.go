package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestLivenessAlwaysOK verifies /healthz reports up regardless of dependencies —
// it must not restart-loop the process over a transient DB blip.
func TestLivenessAlwaysOK(t *testing.T) {
	api := newTestAPI(t)
	rr := api.do(t, http.MethodGet, "/healthz", "", false)
	if rr.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", rr.Code)
	}
}

// TestReadinessReportsHealthy checks that /readyz probes its dependencies and
// reports ready when the DB and blob store are reachable.
func TestReadinessReportsHealthy(t *testing.T) {
	api := newTestAPI(t)
	rr := api.do(t, http.MethodGet, "/readyz", "", false)
	if rr.Code != http.StatusOK {
		t.Fatalf("/readyz = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body.Status != "ready" || body.Checks["database"] != "ok" || body.Checks["blobStore"] != "ok" {
		t.Fatalf("unexpected readiness body: %+v", body)
	}
}

// TestReadinessFailsWhenDBDown verifies /readyz returns 503 with a per-check
// breakdown when the metadata database is unreachable, so an orchestrator stops
// routing traffic to the instance.
func TestReadinessFailsWhenDBDown(t *testing.T) {
	api := newTestAPI(t)
	// Close the DB out from under the router to simulate an outage.
	_ = api.db.Close()

	rr := api.do(t, http.MethodGet, "/readyz", "", false)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with DB down = %d, want 503", rr.Code)
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body.Status != "unready" || body.Checks["database"] != "unavailable" {
		t.Fatalf("expected database unavailable, got: %+v", body)
	}
	// Liveness must still report up even though a dependency is down.
	if live := api.do(t, http.MethodGet, "/healthz", "", false); live.Code != http.StatusOK {
		t.Fatalf("/healthz with DB down = %d, want 200", live.Code)
	}
}
