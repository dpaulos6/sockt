ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS users_verified_email_unique_idx ON users (lower(email)) WHERE email_verified_at IS NOT NULL;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_label VARCHAR(64) NOT NULL DEFAULT 'Legacy device';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE TABLE IF NOT EXISTS recovery_codes (
  code_hash BYTEA PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS recovery_codes_user_unused_idx ON recovery_codes (user_id) WHERE used_at IS NULL;
