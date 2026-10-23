package pagination

import (
	"net/url"
	"strings"
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
	for _, value := range []string{"0", "-1", "101", "not-a-number", " ", "999999999999999999999999999999999999"} {
		if _, err := ParseLimit(value); err == nil {
			t.Fatalf("limit %q accepted", value)
		}
	}
	if _, err := ParseLimit(strings.Repeat("9", 1_000_000)); err == nil {
		t.Fatal("oversized integer limit accepted")
	}
}

func TestParseQueryRejectsDuplicateAndEmptyPaginationParameters(t *testing.T) {
	organizationID := uuid.New()
	for _, values := range []url.Values{
		{"limit": {"10", "20"}},
		{"cursor": {"a", "b"}},
		{"cursor": {""}},
		{"limit": {""}},
		{"status": {"pending", "paid"}},
		{"search": {strings.Repeat("x", MaxQueryValueLength+1)}},
	} {
		if _, _, err := ParseQuery(values, organizationID); err == nil {
			t.Fatalf("query values %v accepted", values)
		}
	}
}

func TestParseQueryDefaultsAndValidatesTenantCursor(t *testing.T) {
	organizationID := uuid.New()
	if limit, cursor, err := ParseQuery(url.Values{}, organizationID); err != nil || limit != DefaultLimit || cursor.ID != uuid.Nil {
		t.Fatalf("default query limit=%d cursor=%+v err=%v", limit, cursor, err)
	}
	encoded := EncodeCursor(Cursor{OrganizationID: organizationID, CreatedAt: time.Now().UTC(), ID: uuid.New()})
	if limit, cursor, err := ParseQuery(url.Values{"limit": {"100"}, "cursor": {encoded}}, organizationID); err != nil || limit != MaxLimit || cursor.ID == uuid.Nil {
		t.Fatalf("valid query limit=%d cursor=%+v err=%v", limit, cursor, err)
	}
	if _, _, err := ParseQuery(url.Values{"cursor": {encoded}}, uuid.New()); err == nil {
		t.Fatal("cross-tenant cursor accepted")
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
	if _, err := DecodeCursor(strings.Repeat("a", MaxCursorLength+1), org); err == nil {
		t.Fatal("oversized cursor accepted")
	}
}
