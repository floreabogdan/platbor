package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
)

// maxTokenTTLDays bounds how far in the future a token may be set to expire.
const maxTokenTTLDays = 3650

// tokensHandler serves /api/v1/tokens — personal access tokens for the
// authenticated user.
type tokensHandler struct {
	svc *auth.Service
	log *slog.Logger
}

func (h tokensHandler) mount(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Delete("/{id}", h.delete)
}

type tokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func toTokenResponse(t auth.Token) tokenResponse {
	return tokenResponse{
		ID:        t.ID,
		Name:      t.Name,
		Prefix:    t.Prefix,
		CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt,
	}
}

type listTokensResponse struct {
	Tokens []tokenResponse `json:"tokens"`
}

func (h tokensHandler) list(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	tokens, err := h.svc.ListTokens(r.Context(), user.ID)
	if err != nil {
		h.log.Error("listing tokens", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, toTokenResponse(t))
	}
	writeJSON(w, h.log, http.StatusOK, listTokensResponse{Tokens: items})
}

type createTokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expiresInDays"` // 0 means no expiry
}

// createTokenResponse includes the raw secret, shown exactly once.
type createTokenResponse struct {
	tokenResponse
	Token string `json:"token"`
}

func (h tokensHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	if req.Name == "" {
		writeProblem(w, http.StatusBadRequest, "Invalid token", "name must not be empty")
		return
	}
	if len(req.Name) > 100 {
		writeProblem(w, http.StatusBadRequest, "Invalid token", "name must be at most 100 characters")
		return
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > maxTokenTTLDays {
		writeProblem(w, http.StatusBadRequest, "Invalid token", "expiresInDays must be between 0 and 3650")
		return
	}

	user, _ := userFromContext(r.Context())
	ttl := time.Duration(req.ExpiresInDays) * 24 * time.Hour

	raw, tok, err := h.svc.CreateToken(r.Context(), user.ID, req.Name, ttl)
	if err != nil {
		h.log.Error("creating token", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusCreated, createTokenResponse{tokenResponse: toTokenResponse(tok), Token: raw})
}

func (h tokensHandler) delete(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	tokenID := chi.URLParam(r, "id")

	if err := h.svc.DeleteToken(r.Context(), user.ID, tokenID); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			writeProblem(w, http.StatusNotFound, "Token not found", "")
			return
		}
		h.log.Error("deleting token", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
