DROP TABLE IF EXISTS auth_sessions;
DROP INDEX IF EXISTS users_email_lower_idx;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
