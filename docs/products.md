# FlowCart Products

Products belong to exactly one organization. Product repository queries always
include `organization_id`, so a product UUID cannot be used to cross a tenant
boundary. Cross-tenant or missing products return `404`.

## Fields and Validation

Products require a trimmed 2-255 character name and a 1-100 character SKU.
SKUs are normalized to uppercase and may contain letters, digits, hyphens,
underscores, and dots. SKU uniqueness is case-insensitive within an
organization, while two organizations may use the same SKU.

Products have an optional description and an `is_active` flag. PATCH supports
partial updates, including deactivation. There is intentionally no DELETE
route: later inventory, order, and reporting records may reference products.

## Permissions

All organization members can list and view products. Owners and admins can
create and update products. Warehouse managers, support users, and viewers
receive `403` for product writes.

Inventory quantities, reservations, stock movements, and order relationships
are intentionally not part of this milestone.
