package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/audit"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry/oci"
)

// activityLimit caps how many recent events the dashboard feed shows.
const activityLimit = 20

// dashboardHandler serves the "everything at a glance" summary: coarse counts
// plus a recent-activity feed drawn from the audit log.
type dashboardHandler struct {
	projects *project.Service
	browser  *oci.Browser
	audit    *audit.Service
	log      *slog.Logger
}

func (h dashboardHandler) mount(r chi.Router) {
	r.Get("/", h.get)
}

type dashboardSummary struct {
	Projects     int `json:"projects"`
	Repositories int `json:"repositories"`
	Tags         int `json:"tags"`
}

type activityEntry struct {
	Actor       string            `json:"actor"`
	Action      string            `json:"action"`
	TargetType  string            `json:"targetType"`
	TargetID    string            `json:"targetId"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ProjectKey  string            `json:"projectKey,omitempty"`
	ProjectName string            `json:"projectName,omitempty"`
	At          time.Time         `json:"at"`
}

type dashboardResponse struct {
	Summary  dashboardSummary `json:"summary"`
	Activity []activityEntry  `json:"activity"`
}

func (h dashboardHandler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projects, err := h.projects.Count(ctx)
	if err != nil {
		h.fail(w, "counting projects", err)
		return
	}
	stats, err := h.browser.Stats(ctx)
	if err != nil {
		h.fail(w, "registry stats", err)
		return
	}
	entries, err := h.audit.Recent(ctx, activityLimit)
	if err != nil {
		h.fail(w, "recent activity", err)
		return
	}

	activity := make([]activityEntry, 0, len(entries))
	for _, e := range entries {
		activity = append(activity, activityEntry{
			Actor:       e.Actor,
			Action:      e.Action,
			TargetType:  e.TargetType,
			TargetID:    e.TargetID,
			Metadata:    e.Metadata,
			ProjectKey:  e.ProjectKey,
			ProjectName: e.ProjectName,
			At:          e.CreatedAt,
		})
	}

	writeJSON(w, h.log, http.StatusOK, dashboardResponse{
		Summary:  dashboardSummary{Projects: projects, Repositories: stats.Repositories, Tags: stats.Tags},
		Activity: activity,
	})
}

func (h dashboardHandler) fail(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}
