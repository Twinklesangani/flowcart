package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Filter struct {
	Limit        int
	Cursor       string
	EventType    string
	ResourceType string
}

type Page struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

type Reader struct{ pool *pgxpool.Pool }

func NewReader(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

func (r *Reader) OrganizationEvents(ctx context.Context, organizationID uuid.UUID, filter Filter) (Page, error) {
	if filter.Limit < 1 || filter.Limit > 100 {
		return Page{}, errors.New("invalid audit limit")
	}
	where := `WHERE organization_id=$1`
	args := []any{organizationID}
	arg := 2
	if filter.EventType != "" {
		where += fmt.Sprintf(" AND event_type=$%d", arg)
		args = append(args, filter.EventType)
		arg++
	}
	if filter.ResourceType != "" {
		where += fmt.Sprintf(" AND resource_type=$%d", arg)
		args = append(args, filter.ResourceType)
		arg++
	}
	if filter.Cursor != "" {
		cursorTime, cursorID, err := decodeTenantCursor(filter.Cursor, organizationID)
		if err != nil {
			return Page{}, errors.New("invalid audit cursor")
		}
		where += fmt.Sprintf(" AND (created_at,id)<($%d,$%d)", arg, arg+1)
		args = append(args, cursorTime, cursorID)
		arg += 2
	}
	args = append(args, filter.Limit+1)
	rows, err := r.pool.Query(ctx, `SELECT id,organization_id,event_type,resource_type,resource_id,parent_resource_type,parent_resource_id,actor_type,actor_user_id,source_type,source_id,metadata,created_at FROM audit_events `+where+fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d`, arg), args...)
	if err != nil {
		return Page{}, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	if err != nil {
		return Page{}, err
	}
	page := Page{Events: events}
	if len(events) > filter.Limit {
		page.Events = events[:filter.Limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor = encodeTenantCursor(organizationID, last.CreatedAt, last.ID)
	}
	return page, nil
}

func (r *Reader) OrderTimeline(ctx context.Context, organizationID, orderID uuid.UUID) ([]Event, error) {
	return r.timeline(ctx, `EXISTS (SELECT 1 FROM orders o WHERE o.organization_id=a.organization_id AND o.id=$2) AND ((a.resource_type='order' AND a.resource_id=$2) OR (a.parent_resource_type='order' AND a.parent_resource_id=$2))`, organizationID, orderID)
}

func (r *Reader) TransferTimeline(ctx context.Context, organizationID, transferID uuid.UUID) ([]Event, error) {
	return r.timeline(ctx, `EXISTS (SELECT 1 FROM inventory_transfers t WHERE t.organization_id=a.organization_id AND t.id=$2) AND a.resource_type='transfer' AND a.resource_id=$2 AND a.event_type IN ('transfer.created','transfer.cancelled','transfer.dispatched','transfer.received')`, organizationID, transferID)
}

func (r *Reader) timeline(ctx context.Context, predicate string, organizationID, resourceID uuid.UUID) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id,a.organization_id,a.event_type,a.resource_type,a.resource_id,a.parent_resource_type,a.parent_resource_id,a.actor_type,a.actor_user_id,a.source_type,a.source_id,a.metadata,a.created_at FROM audit_events a WHERE a.organization_id=$1 AND `+predicate+` ORDER BY a.created_at ASC,a.id ASC`, organizationID, resourceID)
	if err != nil {
		return nil, fmt.Errorf("read audit timeline: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.OrganizationID, &event.EventType, &event.ResourceType, &event.ResourceID, &event.ParentResourceType, &event.ParentResourceID, &event.ActorType, &event.ActorUserID, &event.SourceType, &event.SourceID, &metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return nil, fmt.Errorf("decode audit metadata: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type cursorPayload struct {
	OrganizationID uuid.UUID `json:"organization_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ID             uuid.UUID `json:"id"`
}

func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	payload, _ := json.Marshal(cursorPayload{CreatedAt: createdAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func encodeTenantCursor(organizationID uuid.UUID, createdAt time.Time, id uuid.UUID) string {
	payload, _ := json.Marshal(cursorPayload{OrganizationID: organizationID, CreatedAt: createdAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (time.Time, uuid.UUID, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	var cursor cursorPayload
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return time.Time{}, uuid.Nil, errors.New("invalid cursor")
	}
	return cursor.CreatedAt, cursor.ID, nil
}

func decodeTenantCursor(value string, organizationID uuid.UUID) (time.Time, uuid.UUID, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	var cursor cursorPayload
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OrganizationID != organizationID || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return time.Time{}, uuid.Nil, errors.New("invalid tenant cursor")
	}
	return cursor.CreatedAt, cursor.ID, nil
}
