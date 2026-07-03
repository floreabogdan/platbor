package generic

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

func userFromContext(ctx context.Context) (auth.User, bool) {
	user, ok := ctx.Value(userContextKey).(auth.User)
	return user, ok
}

// actorFrom returns the authenticated username for audit records; serve attaches
// a user before any mutating handler runs.
func actorFrom(r *http.Request) string {
	if user, ok := userFromContext(r.Context()); ok {
		return user.Username
	}
	return "system"
}
