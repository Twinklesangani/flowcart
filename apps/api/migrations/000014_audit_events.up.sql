BEGIN;

CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    parent_resource_type TEXT,
    parent_resource_id UUID,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user','system','provider')),
    actor_user_id UUID,
    source_type TEXT NOT NULL,
    source_id UUID NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (actor_type = 'user' AND actor_user_id IS NOT NULL)
        OR (actor_type IN ('system','provider') AND actor_user_id IS NULL)
    )
);

CREATE UNIQUE INDEX audit_events_org_source_identity_idx
    ON audit_events (organization_id, source_type, source_id, event_type);

CREATE INDEX audit_events_organization_created_idx
    ON audit_events (organization_id, created_at DESC, id DESC);

CREATE INDEX audit_events_resource_timeline_idx
    ON audit_events (organization_id, resource_type, resource_id, created_at ASC, id ASC);

CREATE INDEX audit_events_parent_timeline_idx
    ON audit_events (organization_id, parent_resource_type, parent_resource_id, created_at ASC, id ASC);

CREATE INDEX audit_events_event_type_idx
    ON audit_events (event_type, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION audit_events_append_only() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit_events are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_append_only_trg
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

COMMIT;
