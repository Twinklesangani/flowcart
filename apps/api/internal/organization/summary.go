package organization

import "github.com/google/uuid"

type OrganizationSummary struct {
	ID   uuid.UUID
	Name string
	Slug string
	Role Role
}
