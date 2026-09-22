package product

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type ListPage struct {
	Products   []Product `json:"products"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

type Product struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	SKU            string    `json:"sku"`
	Name           string    `json:"name"`
	Description    *string   `json:"description"`
	IsActive       bool      `json:"is_active"`
	UnitPriceMinor *int64    `json:"unit_price_minor"`
	CurrencyCode   *string   `json:"currency_code"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateInput struct {
	SKU            string  `json:"sku"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	IsActive       *bool   `json:"is_active"`
	UnitPriceMinor *int64  `json:"unit_price_minor"`
	CurrencyCode   *string `json:"currency_code"`
}

type PatchInput struct {
	SKU            *string `json:"sku"`
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	IsActive       *bool   `json:"is_active"`
	UnitPriceMinor *int64  `json:"unit_price_minor"`
	CurrencyCode   *string `json:"currency_code"`
}

var supportedCurrencies = map[string]bool{"AUD": true, "USD": true, "INR": true}

func normalizeCurrency(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(*value))
	return &normalized
}

func validCurrency(value *string) bool {
	return value != nil && supportedCurrencies[*value]
}

func normalizeSKU(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }
func validSKU(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}
func validName(value string) bool { return len(value) >= 2 && len(value) <= 255 }
