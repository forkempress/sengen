// Package tenant carries the current request's firm through the request
// context. Every multi-tenant table is scoped by tenant_id; which firm a
// request belongs to is decided in exactly one place — auth.RequireAuth,
// from the signed-in session — and read back here by the handlers.
package tenant

import (
	"context"

	"shengen/internal/store"
)

type ctxKey int

const tenantCtxKey ctxKey = iota

func WithTenant(ctx context.Context, t *store.Tenant) context.Context {
	return context.WithValue(ctx, tenantCtxKey, t)
}

func FromContext(ctx context.Context) (*store.Tenant, bool) {
	t, ok := ctx.Value(tenantCtxKey).(*store.Tenant)
	return t, ok
}
