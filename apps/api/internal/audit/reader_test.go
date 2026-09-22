package audit

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	cursor := encodeCursor(createdAt, id)
	gotTime, gotID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(createdAt) || gotID != id {
		t.Fatalf("cursor = %s %s", gotTime, gotID)
	}
}

func TestAuditCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "not-base64", "e30"} {
		if _, _, err := decodeCursor(value); err == nil {
			t.Fatalf("cursor %q was accepted", value)
		}
	}
}

func TestAuditTenantCursorRejectsAnotherOrganization(t *testing.T) {
	cursor := encodeTenantCursor(uuid.New(), time.Now().UTC(), uuid.New())
	if _, _, err := decodeTenantCursor(cursor, uuid.New()); err == nil {
		t.Fatal("cursor from another organization was accepted")
	}
}
