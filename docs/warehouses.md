# FlowCart Warehouses

Warehouses belong to exactly one organization. All warehouse repository queries
include `organization_id`; cross-tenant or missing warehouse access returns
`404` without revealing whether the record exists.

## Fields and Validation

Warehouses require a trimmed 2-255 character name and a 1-50 character code.
Codes are normalized to uppercase and may contain letters, digits, hyphens, and
underscores. Code uniqueness is case-insensitive within an organization, while
two organizations may use the same code.

`country_code`, when present, is normalized to two uppercase alphabetic
characters. Address fields are optional. PATCH is partial and can toggle
`is_active`. There is intentionally no DELETE route because warehouses may be
referenced by future inventory, transfer, and order records.

## Permissions

All organization members can list and view warehouses. Owners, admins, and
warehouse managers can create and update warehouse metadata. Support users and
viewers receive `403` for warehouse writes. Warehouse managers can maintain
physical warehouse metadata but cannot change the product catalog.

Inventory quantities, reservations, transfers, stock movements, and orders are
intentionally not part of this milestone.
