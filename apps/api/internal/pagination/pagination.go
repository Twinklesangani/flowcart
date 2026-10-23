package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultLimit        = 50
	MaxLimit            = 100
	MaxCursorLength     = 256
	MaxQueryValueLength = 256
)

type Cursor struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	CreatedAt      time.Time `json:"created_at"`
	ID             uuid.UUID `json:"id"`
}

func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 3 {
		return 0, errors.New("invalid limit")
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxLimit {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}

func ParseQuery(values url.Values, organizationID uuid.UUID) (int, Cursor, error) {
	if err := validateSingleValues(values); err != nil {
		return 0, Cursor{}, err
	}
	limit := DefaultLimit
	if raw, ok := values["limit"]; ok {
		if strings.TrimSpace(raw[0]) == "" {
			return 0, Cursor{}, errors.New("invalid limit")
		}
		var err error
		limit, err = ParseLimit(raw[0])
		if err != nil {
			return 0, Cursor{}, err
		}
	}
	var cursor Cursor
	if raw, ok := values["cursor"]; ok {
		if raw[0] == "" {
			return 0, Cursor{}, errors.New("invalid cursor")
		}
		var err error
		cursor, err = DecodeCursor(raw[0], organizationID)
		if err != nil {
			return 0, Cursor{}, err
		}
	}
	return limit, cursor, nil
}

func ParseLimitValues(values url.Values) (int, error) {
	if err := validateSingleValues(values); err != nil {
		return 0, err
	}
	raw, ok := values["limit"]
	if !ok {
		return DefaultLimit, nil
	}
	if strings.TrimSpace(raw[0]) == "" {
		return 0, errors.New("invalid limit")
	}
	return ParseLimit(raw[0])
}

func SingleValue(values url.Values, key string) (string, bool, error) {
	if err := validateSingleValues(values); err != nil {
		return "", false, err
	}
	items, ok := values[key]
	if !ok {
		return "", false, nil
	}
	return items[0], true, nil
}

func validateSingleValues(values url.Values) error {
	for _, items := range values {
		if len(items) != 1 {
			return errors.New("duplicate query parameter")
		}
		if len(items[0]) > MaxQueryValueLength {
			return errors.New("query parameter is too long")
		}
	}
	return nil
}

func EncodeCursor(cursor Cursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeCursor(raw string, organizationID uuid.UUID) (Cursor, error) {
	if raw == "" || len(raw) > MaxCursorLength {
		return Cursor{}, errors.New("invalid cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, errors.New("invalid cursor")
	}
	var cursor Cursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OrganizationID != organizationID || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return Cursor{}, errors.New("invalid cursor")
	}
	return cursor, nil
}
