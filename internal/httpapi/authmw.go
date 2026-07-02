package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/platbor/platbor/internal/core/auth"
)

// authenticate attaches the user to the request context using either an
// `Authorization: Bearer <token>` header (machines/CLI) or the session cookie
// (browsers). A missing or invalid credential is not an error here — requests
// proceed anonymously, and requireUser decides what needs a login.
func authenticate(svc *auth.Service, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user, ok := resolveIdentity(r, svc, log); ok {
				r = r.WithContext(withUser(r.Context(), user))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveIdentity tries a bearer token first, then the session cookie.
func resolveIdentity(r *http.Request, svc *auth.Service, log *slog.Logger) (auth.User, bool) {
	if token, ok := bearerToken(r); ok {
		user, err := svc.AuthenticateToken(r.Context(), token)
		if err != nil {
			if !errors.Is(err, auth.ErrInvalidToken) {
				log.Error("authenticating token", slog.String("error", err.Error()))
			}
			return auth.User{}, false
		}
		return user, true
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return auth.User{}, false
	}
	user, err := svc.ResolveSession(r.Context(), cookie.Value)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			log.Error("resolving session", slog.String("error", err.Error()))
		}
		return auth.User{}, false
	}
	return user, true
}

// bearerToken extracts a token from an Authorization: Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	return header[len(prefix):], true
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

// requireAdmin rejects requests whose user is not an instance admin. It layers
// on top of requireUser, so an absent user is a 401 and a non-admin is a 403.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFromContext(r.Context())
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "Not authenticated", "a valid session is required")
			return
		}
		if !user.IsAdmin {
			writeProblem(w, http.StatusForbidden, "Forbidden", "instance admin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
