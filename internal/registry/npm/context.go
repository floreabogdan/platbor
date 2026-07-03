package npm

import (
	"context"
	"net/http"

	"github.com/platbor/platbor/internal/core/auth"
)

type contextKey int

const userContextKey contextKey = iota

func withUser(ctx context.Context, user auth.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// userFromContext returns the authenticated user attached by serve.
func userFromContext(ctx context.Context) (auth.User, bool) {
	user, ok := ctx.Value(userContextKey).(auth.User)
	return user, ok
}

// actorFrom returns the authenticated username for audit records. serve always
// attaches a user before a mutating handler runs; "system" is a defensive
// fallback.
func actorFrom(r *http.Request) string {
	if user, ok := userFromContext(r.Context()); ok {
		return user.Username
	}
	return "system"
}
