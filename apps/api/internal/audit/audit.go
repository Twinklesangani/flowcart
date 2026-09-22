package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Event struct {
	ID                 uuid.UUID      `json:"id"`
	OrganizationID     uuid.UUID      `json:"organization_id"`
	EventType          string         `json:"event_type"`
	ResourceType       string         `json:"resource_type"`
	ResourceID         uuid.UUID      `json:"resource_id"`
	ParentResourceType *string        `json:"parent_resource_type,omitempty"`
	ParentResourceID   *uuid.UUID     `json:"parent_resource_id,omitempty"`
	ActorType          string         `json:"actor_type"`
	ActorUserID        *uuid.UUID     `json:"actor_user_id,omitempty"`
	SourceType         string         `json:"source_type"`
	SourceID           uuid.UUID      `json:"source_id"`
	Metadata           map[string]any `json:"metadata"`
	CreatedAt          time.Time      `json:"created_at"`
}

type Writer struct{}

func (w Writer) AppendEvent(ctx context.Context, tx pgx.Tx, event Event) error {
	if tx == nil {
		return errors.New("audit transaction required")
	}
	if event.ActorType == "user" && (event.ActorUserID == nil || *event.ActorUserID == uuid.Nil) {
		return errors.New("user audit events require actor_user_id")
	}
	if event.ActorType != "user" && event.ActorUserID != nil && *event.ActorUserID != uuid.Nil {
		return errors.New("actor_user_id is only valid for user actor_type")
	}
	if event.EventType == "" || event.ResourceType == "" || event.ResourceID == uuid.Nil || event.SourceType == "" || event.SourceID == uuid.Nil || event.ActorType == "" {
		return errors.New("audit event requires required fields")
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(SanitizeMetadata(event.Metadata))
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	var actorUserID any
	if event.ActorUserID != nil {
		actorUserID = *event.ActorUserID
	}
	var parentType any
	if event.ParentResourceType != nil {
		parentType = *event.ParentResourceType
	}
	var parentID any
	if event.ParentResourceID != nil {
		parentID = *event.ParentResourceID
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (organization_id,event_type,resource_type,resource_id,parent_resource_type,parent_resource_id,actor_type,actor_user_id,source_type,source_id,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (organization_id,source_type,source_id,event_type) DO NOTHING`, event.OrganizationID, event.EventType, event.ResourceType, event.ResourceID, parentType, parentID, event.ActorType, actorUserID, event.SourceType, event.SourceID, metadata)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func sanitizeMetadata(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	clean := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(key)
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "jwt") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "client_secret") || strings.Contains(lower, "request_hash") || strings.Contains(lower, "webhook_body") || strings.Contains(lower, "raw_body") || strings.Contains(lower, "raw_webhook") || strings.Contains(lower, "payload") {
			continue
		}
		clean[key] = value
	}
	return clean
}

func SanitizeMetadata(input map[string]any) map[string]any {
	return sanitizeMetadata(input)
}

func UserEvent(orgID, resourceID, sourceID uuid.UUID, eventType string, actorUserID uuid.UUID) Event {
	return Event{
		OrganizationID: orgID,
		EventType:      eventType,
		ResourceType:   "order",
		ResourceID:     resourceID,
		ActorType:      "user",
		ActorUserID:    &actorUserID,
		SourceType:     "order",
		SourceID:       sourceID,
		Metadata:       map[string]any{},
	}
}
