DROP TABLE IF EXISTS recovery_codes;
ALTER TABLE sessions DROP COLUMN IF EXISTS last_seen_at;
ALTER TABLE sessions DROP COLUMN IF EXISTS device_label;
DROP INDEX IF EXISTS users_verified_email_unique_idx;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS email;
