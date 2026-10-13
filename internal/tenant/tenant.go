// Package tenant resolves the current request's tenant from the HTTP Host
// header and carries it through the request context. Every multi-tenant
// table is scoped by tenant_id, and this is the single place that decides
// which tenant a request belongs to.
package tenant

import (
	"context"
	"net/http"
	"strings"

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

// Middleware resolves the tenant for every request by looking up the Host
// header (stripped of port) in the tenants table. Unknown hosts get a plain
// 404 — there is no cross-tenant fallback.
func Middleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if i := strings.IndexByte(host, ':'); i != -1 {
				host = host[:i]
			}
			t, err := st.GetTenantByHost(r.Context(), host)
			if err != nil {
				http.Error(w, "nepoznat domen", http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), t)))
		})
	}
}
