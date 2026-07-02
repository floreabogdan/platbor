package httpapi

import (
	"context"

	"github.com/platbor/platbor/internal/core/auth"
)

// contextKey is unexported so no other package can collide with our context values.
type contextKey int

const userContextKey contextKey = iota

// withUser returns a copy of ctx carrying the authenticated user.
func withUser(ctx context.Context, user auth.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// userFromContext returns the authenticated user, if the request carried a
// valid session.
func userFromContext(ctx context.Context) (auth.User, bool) {
	user, ok := ctx.Value(userContextKey).(auth.User)
	return user, ok
}
