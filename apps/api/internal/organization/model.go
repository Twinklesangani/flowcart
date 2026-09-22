package organization

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleOwner            Role = "owner"
	RoleAdmin            Role = "admin"
	RoleWarehouseManager Role = "warehouse_manager"
	RoleSupport          Role = "support"
	RoleViewer           Role = "viewer"
)

func validRole(role Role) bool {
	switch role {
	case RoleOwner, RoleAdmin, RoleWarehouseManager, RoleSupport, RoleViewer:
		return true
	}
	return false
}

type Organization struct {
	ID   uuid.UUID
	Name string
	Slug string
}
type OrganizationMembership struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           Role
	CreatedAt      time.Time
}
type Member struct {
	UserID                     uuid.UUID
	Email, FirstName, LastName string
	Role                       Role
	CreatedAt                  time.Time
}
type TenantContext struct {
	OrganizationID, UserID uuid.UUID
	Role                   Role
}
type tenantContextKey string

const tenantKey tenantContextKey = "organization-tenant-context"

func withTenant(ctx context.Context, tenant TenantContext) context.Context {
	return context.WithValue(ctx, tenantKey, tenant)
}
func WithTenant(ctx context.Context, tenant TenantContext) context.Context {
	return withTenant(ctx, tenant)
}
func TenantFromContext(ctx context.Context) (TenantContext, bool) {
	tenant, ok := ctx.Value(tenantKey).(TenantContext)
	return tenant, ok
}
