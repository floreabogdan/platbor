package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/platbor/platbor/internal/core/auth"
)

// loadSession resolves the session cookie (if any) and attaches the user to the
// request context. A missing or invalid cookie is not an error here — requests
// simply proceed anonymously, and requireUser decides what needs a login.
func loadSession(svc *auth.Service, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			user, err := svc.ResolveSession(r.Context(), cookie.Value)
			if err != nil {
				if !errors.Is(err, auth.ErrNoSession) {
					log.Error("resolving session", slog.String("error", err.Error()))
				}
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
		})
	}
}

// requireUser rejects requests that have no authenticated user in context.
func requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := userFromContext(r.Context()); !ok {
			writeProblem(w, http.StatusUnauthorized, "Not authenticated", "a valid session is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
