BEGIN;
-- Revocation timestamps intentionally survive rollback.
ALTER TABLE auth_sessions DROP COLUMN family_id;
DROP TABLE auth_session_families;
COMMIT;
