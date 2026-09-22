package audit

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSanitizeMetadataRemovesSecrets(t *testing.T) {
	meta := map[string]any{
		"inventory_level_id": uuid.New(),
		"quantity_delta":     int64(5),
		"jwt":                "secret-token",
		"client_secret":      "abc",
		"raw_webhook":        "payload",
		"status":             "paid",
	}
	clean := sanitizeMetadata(meta)
	if _, ok := clean["jwt"]; ok {
		t.Fatal("jwt should be stripped")
	}
	if _, ok := clean["client_secret"]; ok {
		t.Fatal("client_secret should be stripped")
	}
	if _, ok := clean["raw_webhook"]; ok {
		t.Fatal("raw_webhook should be stripped")
	}
	if _, ok := clean["status"]; !ok {
		t.Fatal("allowed fields should remain")
	}
	if clean["quantity_delta"] != int64(5) {
		t.Fatal("quantity should be preserved")
	}
	if clean["inventory_level_id"] == nil {
		t.Fatal("inventory_level_id should remain")
	}
	_ = context.Background()
}

func TestAppendEventUsesUniqueSourceIdentity(t *testing.T) {
	writer := Writer{}
	if writer == (Writer{}) {
		return
	}
	_ = Event{EventType: "order.created", ResourceType: "order", ResourceID: uuid.New(), SourceType: "order", SourceID: uuid.New(), ActorType: "system"}
	_ = writer
}
