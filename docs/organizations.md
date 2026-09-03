# FlowCart OS Organizations

## Organization Model

An organization is a tenant boundary. Users access organization data only
through a row in `organization_members`. Creating an organization creates its
owner membership in the same PostgreSQL transaction.

The current model contains:

- `organizations`: organization identity and slug
- `organization_members`: user membership and role
- `users`: authenticated identities

There is no organization email invitation system. Adding a member accepts the
email of an existing FlowCart user.

## Tenant Context

Organization routes use an organization ID in the URL:

```text
/api/v1/organizations/{organizationID}/...
```

The middleware first reads the authenticated user ID from the access-token
middleware, then verifies the route organization ID against PostgreSQL
membership. Only after that lookup does it place this context into the request:

- organization ID
- user ID
- membership role

Repositories scope organization-specific queries by organization ID. The
frontend-supplied organization ID is never trusted without membership
verification. JWTs prove identity only and are not the source of role data.

For organization-specific access, a non-member receives `404 Organization not
found`. This consistent policy avoids unnecessarily revealing whether an
inaccessible organization exists. Insufficient permissions for a known member
return `403 Forbidden`.

## Roles and Capabilities

The fixed roles are:

- `owner`: full organization administration, including owner management
- `admin`: organization updates and non-owner member management
- `warehouse_manager`: view organization and members only
- `support`: view organization and members only
- `viewer`: view organization and members only

There are no custom roles or permission database tables yet.

Only owners can assign `owner`. Admins may assign `admin`,
`warehouse_manager`, `support`, or `viewer`, but cannot modify an owner or
promote anyone to owner.

An organization must always retain at least one owner. Role changes and member
removals that could affect the last owner use a PostgreSQL transaction that
locks the organization row before looking up or changing memberships. The
last owner cannot be demoted or removed.

## API Endpoints

All endpoints require an access JWT. Organization-specific endpoints also
require verified membership.

```text
POST   /api/v1/organizations
GET    /api/v1/organizations
GET    /api/v1/organizations/{organizationID}
PATCH  /api/v1/organizations/{organizationID}

GET    /api/v1/organizations/{organizationID}/members
POST   /api/v1/organizations/{organizationID}/members
PATCH  /api/v1/organizations/{organizationID}/members/{userID}
DELETE /api/v1/organizations/{organizationID}/members/{userID}
```

`POST /organizations` accepts `name` and `slug`, creates the organization, and
automatically creates the authenticated user as `owner`.

`GET /organizations` returns only organizations where the authenticated user
has membership and includes the membership role.

Member list responses contain user ID, email, names, role, and membership
creation time. They never expose password hashes, auth sessions, or refresh
Token hashes.

## Not Implemented

Products, warehouses, inventory, orders, organization email invitations,
billing, subscriptions, custom permissions, audit logs, and other business
features are not implemented.
