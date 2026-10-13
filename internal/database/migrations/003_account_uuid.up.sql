-- Additive identity bridge. Keep legacy keys and every integer FK unchanged.
ALTER TABLE users ADD COLUMN account_uuid UUID;
ALTER TABLE users ALTER COLUMN account_uuid SET DEFAULT gen_random_uuid();
UPDATE users SET account_uuid=gen_random_uuid() WHERE account_uuid IS NULL;
ALTER TABLE users ALTER COLUMN account_uuid SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_account_uuid_unique UNIQUE (account_uuid);
