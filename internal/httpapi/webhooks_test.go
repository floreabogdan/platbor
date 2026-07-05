package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestWebhookCRUD exercises the project webhook endpoints: create returns the
// signing secret once, list redacts it, and delete removes it.
func TestWebhookCRUD(t *testing.T) {
	api := newTestAPI(t)
	if rr := api.post(t, "/api/v1/projects", `{"key":"w","name":"W"}`); rr.Code != http.StatusCreated {
		t.Fatalf("create project: %d", rr.Code)
	}

	// Create: secret is returned exactly once.
	rr := api.post(t, "/api/v1/projects/w/webhooks", `{"url":"https://example.com/hook","events":"oci.,generic."}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create webhook: %d (%s)", rr.Code, rr.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Events string `json:"events"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.Secret == "" || created.URL != "https://example.com/hook" || created.Events != "oci.,generic." {
		t.Fatalf("unexpected create response: %+v", created)
	}

	// List: the secret is not exposed.
	lr := api.do(t, http.MethodGet, "/api/v1/projects/w/webhooks", "", true)
	if lr.Code != http.StatusOK {
		t.Fatalf("list: %d", lr.Code)
	}
	var list struct {
		Webhooks []struct {
			ID     string `json:"id"`
			Secret string `json:"secret"`
		} `json:"webhooks"`
	}
	if err := json.Unmarshal(lr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(list.Webhooks))
	}
	if list.Webhooks[0].Secret != "" {
		t.Errorf("list must not expose the secret, got %q", list.Webhooks[0].Secret)
	}

	// Invalid URL is rejected.
	if rr := api.post(t, "/api/v1/projects/w/webhooks", `{"url":"not-a-url"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("invalid url: %d, want 400", rr.Code)
	}

	// Delete.
	if dr := api.do(t, http.MethodDelete, "/api/v1/projects/w/webhooks/"+created.ID, "", true); dr.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", dr.Code)
	}
	lr2 := api.do(t, http.MethodGet, "/api/v1/projects/w/webhooks", "", true)
	var list2 struct {
		Webhooks []json.RawMessage `json:"webhooks"`
	}
	_ = json.Unmarshal(lr2.Body.Bytes(), &list2)
	if len(list2.Webhooks) != 0 {
		t.Errorf("expected no webhooks after delete, got %d", len(list2.Webhooks))
	}
}
