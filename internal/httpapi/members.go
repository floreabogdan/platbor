package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/project"
)

// membersHandler serves /api/v1/projects/{project}/members: who can access a
// project and with what role. Every route requires the project admin role (or an
// instance admin) — enforced by the requireProjectManage middleware at mount.
type membersHandler struct {
	auth     *auth.Service
	projects *project.Service
	log      *slog.Logger
}

func (h membersHandler) mount(r chi.Router) {
	r.Use(requireProjectManage(h.projects, h.auth))
	r.Get("/", h.list)
	r.Put("/{username}", h.set)
	r.Delete("/{username}", h.remove)
}

type memberResponse struct {
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type listMembersResponse struct {
	Members []memberResponse `json:"members"`
}

func (h membersHandler) list(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	members, err := h.auth.ListMembers(r.Context(), proj.ID)
	if err != nil {
		h.log.Error("listing members", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]memberResponse, 0, len(members))
	for _, m := range members {
		items = append(items, memberResponse{
			Username: m.Username, Email: m.Email, Role: string(m.Role),
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	writeJSON(w, h.log, http.StatusOK, listMembersResponse{Members: items})
}

type setMemberRequest struct {
	Role string `json:"role"`
}

// set grants or updates a user's role in the project.
func (h membersHandler) set(w http.ResponseWriter, r *http.Request) {
	var req setMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	role := auth.Role(req.Role)
	if !auth.ValidRole(role) {
		writeProblem(w, http.StatusBadRequest, "Invalid role", "role must be reader, maintainer, or admin")
		return
	}
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	user, err := h.auth.UserByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeProblem(w, http.StatusNotFound, "User not found", "no such user")
			return
		}
		h.log.Error("resolving member user", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	if err := h.auth.SetMember(r.Context(), proj.ID, user.ID, role); err != nil {
		h.log.Error("setting member", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, memberResponse{
		Username: user.Username, Email: user.Email, Role: string(role),
	})
}

// remove revokes a user's membership.
func (h membersHandler) remove(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	user, err := h.auth.UserByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeProblem(w, http.StatusNotFound, "User not found", "no such user")
			return
		}
		h.log.Error("resolving member user", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	removed, err := h.auth.RemoveMember(r.Context(), proj.ID, user.ID)
	if err != nil {
		h.log.Error("removing member", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	if !removed {
		writeProblem(w, http.StatusNotFound, "Not a member", "that user is not a member of this project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h membersHandler) projectError(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Project not found", "no such project")
		return
	}
	h.log.Error("resolving project", slog.String("error", err.Error()))
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}
