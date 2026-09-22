package warehouse

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type ListPage struct {
	Warehouses []Warehouse `json:"warehouses"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

type Warehouse struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	AddressLine1   *string   `json:"address_line1"`
	AddressLine2   *string   `json:"address_line2"`
	City           *string   `json:"city"`
	State          *string   `json:"state"`
	PostalCode     *string   `json:"postal_code"`
	CountryCode    *string   `json:"country_code"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateInput struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	AddressLine1 *string `json:"address_line1"`
	AddressLine2 *string `json:"address_line2"`
	City         *string `json:"city"`
	State        *string `json:"state"`
	PostalCode   *string `json:"postal_code"`
	CountryCode  *string `json:"country_code"`
	IsActive     *bool   `json:"is_active"`
}

type PatchInput struct {
	Code         *string `json:"code"`
	Name         *string `json:"name"`
	AddressLine1 *string `json:"address_line1"`
	AddressLine2 *string `json:"address_line2"`
	City         *string `json:"city"`
	State        *string `json:"state"`
	PostalCode   *string `json:"postal_code"`
	CountryCode  *string `json:"country_code"`
	IsActive     *bool   `json:"is_active"`
}

func normalizeCode(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }
func normalizeCountryCode(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(*value))
	return &normalized
}
func validCode(value string) bool {
	if len(value) < 1 || len(value) > 50 {
		return false
	}
	for _, character := range value {
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func validName(value string) bool { return len(value) >= 2 && len(value) <= 255 }
func validCountryCode(value *string) bool {
	if value == nil {
		return true
	}
	return len(*value) == 2 && (*value)[0] >= 'A' && (*value)[0] <= 'Z' && (*value)[1] >= 'A' && (*value)[1] <= 'Z'
}
