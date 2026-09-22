package pagination

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLimitDefaultsAndBounds(t *testing.T) {
	if got, err := ParseLimit(""); err != nil || got != DefaultLimit {
		t.Fatalf("default limit=%d err=%v", got, err)
	}
	if got, err := ParseLimit("100"); err != nil || got != MaxLimit {
		t.Fatalf("max limit=%d err=%v", got, err)
	}
	for _, value := range []string{"0", "-1", "101", "not-a-number"} {
		if _, err := ParseLimit(value); err == nil {
			t.Fatalf("limit %q accepted", value)
		}
	}
}

func TestCursorIsOpaqueAndTenantBound(t *testing.T) {
	org := uuid.New()
	cursor := Cursor{OrganizationID: org, CreatedAt: time.Now().UTC(), ID: uuid.New()}
	encoded := EncodeCursor(cursor)
	decoded, err := DecodeCursor(encoded, org)
	if err != nil || decoded.ID != cursor.ID {
		t.Fatalf("decoded cursor=%+v err=%v", decoded, err)
	}
	if _, err := DecodeCursor(encoded, uuid.New()); err == nil {
		t.Fatal("cross-tenant cursor accepted")
	}
	if _, err := DecodeCursor("malformed", org); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}
