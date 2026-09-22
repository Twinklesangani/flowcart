package inventory

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

type lowStockCursor struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Priority       int       `json:"priority"`
	Available      int64     `json:"available"`
	ID             uuid.UUID `json:"id"`
}

func encodeLowStockCursor(organizationID uuid.UUID, cursor lowStockCursor) string {
	cursor.OrganizationID = organizationID
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeLowStockCursor(raw string, organizationID uuid.UUID) (lowStockCursor, error) {
	if raw == "" {
		return lowStockCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return lowStockCursor{}, errors.New("invalid low-stock cursor")
	}
	var cursor lowStockCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OrganizationID != organizationID || cursor.ID == uuid.Nil || (cursor.Priority != 0 && cursor.Priority != 1) {
		return lowStockCursor{}, errors.New("invalid low-stock cursor")
	}
	return cursor, nil
}
