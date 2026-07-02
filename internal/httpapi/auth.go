package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
)

// sessionCookieName is the cookie carrying the opaque session token.
const sessionCookieName = "platbor_session"

// authHandler serves login/logout/me and owns session-cookie mechanics.
type authHandler struct {
	svc          *auth.Service
	log          *slog.Logger
	cookieSecure bool
}

func (h authHandler) mountPublic(r chi.Router) {
	r.Post("/login", h.login)
}

func (h authHandler) mountAuthed(r chi.Router) {
	r.Post("/logout", h.logout)
	r.Get("/me", h.me)
}

// userResponse is the API view of the current user (never includes the hash).
type userResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"isAdmin"`
	CreatedAt time.Time `json:"createdAt"`
}

func toUserResponse(u auth.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		IsAdmin:   u.IsAdmin,
		CreatedAt: u.CreatedAt,
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h authHandler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	user, err := h.svc.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeProblem(w, http.StatusUnauthorized, "Invalid credentials", "username or password is incorrect")
			return
		}
		h.log.Error("authenticating", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	token, expiresAt, err := h.svc.StartSession(r.Context(), user.ID)
	if err != nil {
		h.log.Error("starting session", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	h.setSessionCookie(w, token, expiresAt)
	writeJSON(w, h.log, http.StatusOK, toUserResponse(user))
}

func (h authHandler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := h.svc.EndSession(r.Context(), c.Value); err != nil {
			h.log.Error("ending session", slog.String("error", err.Error()))
		}
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Not authenticated", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, toUserResponse(user))
}

func (h authHandler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h authHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
