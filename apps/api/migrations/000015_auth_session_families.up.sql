BEGIN;

CREATE TABLE auth_session_families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

ALTER TABLE auth_sessions ADD COLUMN family_id UUID;

-- Historical rotation links were never stored. Each legacy row receives an
-- independent, revoked family; all legacy refresh credentials require login.
UPDATE auth_sessions SET family_id = gen_random_uuid();
INSERT INTO auth_session_families (id, user_id, created_at, revoked_at)
SELECT family_id, user_id, created_at, NOW() FROM auth_sessions;
UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, NOW());

ALTER TABLE auth_sessions ADD CONSTRAINT auth_sessions_family_id_fkey
    FOREIGN KEY (family_id) REFERENCES auth_session_families(id);
CREATE INDEX auth_sessions_family_id_idx ON auth_sessions (family_id);
ALTER TABLE auth_sessions ALTER COLUMN family_id SET NOT NULL;

COMMIT;
