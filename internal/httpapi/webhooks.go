package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/webhook"
)

// webhooksHandler serves /api/v1/projects/{project}/webhooks: the project's
// event subscriptions. Every route requires the project admin role (or an
// instance admin), enforced by requireProjectManage at mount. The signing secret
// is returned only when a webhook is created.
type webhooksHandler struct {
	svc      *webhook.Service
	projects *project.Service
	auth     *auth.Service
	log      *slog.Logger
}

func (h webhooksHandler) mount(r chi.Router) {
	r.Use(requireProjectManage(h.projects, h.auth))
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Delete("/{id}", h.remove)
}

type webhookResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    string    `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	// Secret is populated only in the create response, shown once.
	Secret string `json:"secret,omitempty"`
}

func toWebhookResponse(w webhook.Webhook, withSecret bool) webhookResponse {
	resp := webhookResponse{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active, CreatedAt: w.CreatedAt}
	if withSecret {
		resp.Secret = w.Secret
	}
	return resp
}

type listWebhooksResponse struct {
	Webhooks []webhookResponse `json:"webhooks"`
}

func (h webhooksHandler) list(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	hooks, err := h.svc.List(r.Context(), proj.ID)
	if err != nil {
		h.log.Error("listing webhooks", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]webhookResponse, 0, len(hooks))
	for _, hook := range hooks {
		items = append(items, toWebhookResponse(hook, false))
	}
	writeJSON(w, h.log, http.StatusOK, listWebhooksResponse{Webhooks: items})
}

type createWebhookRequest struct {
	URL    string `json:"url"`
	Events string `json:"events,omitempty"` // comma-separated action prefixes, or "*" (default)
	Secret string `json:"secret,omitempty"` // optional; generated when empty
}

func (h webhooksHandler) create(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	var req createWebhookRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	hook, err := h.svc.Create(r.Context(), proj.ID, req.URL, req.Events, req.Secret)
	if err != nil {
		if errors.Is(err, webhook.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Invalid webhook", err.Error())
			return
		}
		h.log.Error("creating webhook", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusCreated, toWebhookResponse(hook, true))
}

func (h webhooksHandler) remove(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	if err := h.svc.Delete(r.Context(), proj.ID, chi.URLParam(r, "id")); err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Webhook not found", "no such webhook in this project")
			return
		}
		h.log.Error("deleting webhook", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h webhooksHandler) projectError(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Project not found", "no project with that key")
		return
	}
	h.log.Error("resolving project", slog.String("error", err.Error()))
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}
