package oci

import (
	"context"

	"github.com/platbor/platbor/internal/core/auth"
)

type contextKey int

const userContextKey contextKey = iota

func withUser(ctx context.Context, user auth.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// userFromContext returns the authenticated user attached by requireAuth.
func userFromContext(ctx context.Context) (auth.User, bool) {
	user, ok := ctx.Value(userContextKey).(auth.User)
	return user, ok
}
