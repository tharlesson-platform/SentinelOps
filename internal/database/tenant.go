package database

import (
	"context"

	"github.com/google/uuid"
)

type tenantContextKey struct{}

// WithTenant returns a context that scopes every PostgreSQL connection
// acquisition to one organization. Invalid identifiers deliberately become an
// empty tenant, which is denied by the database RLS policies.
func WithTenant(ctx context.Context, organizationID string) context.Context {
	if uuid.Validate(organizationID) != nil {
		organizationID = ""
	}
	return context.WithValue(ctx, tenantContextKey{}, organizationID)
}

func tenantFromContext(ctx context.Context) string {
	tenant, _ := ctx.Value(tenantContextKey{}).(string)
	return tenant
}

func withMigrationTenant(ctx context.Context) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, "*")
}
