package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Each migration test gets an independent schema in the explicitly disposable DB.
func uuidTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SOCKT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SOCKT_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	base, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable database failed")
	}
	t.Cleanup(func() { base.Close() })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("uuid_test_%x", random)
	if _, err := base.db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error("test schema cleanup failed")
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		t.Fatal("test database must use a PostgreSQL URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal("open isolated schema failed")
	}
	t.Cleanup(func() { store.Close() })
	// Install exactly the pre-UUID schema and its migration ledger.
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_initial.up.sql", "002_accounts.up.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := store.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestUUIDMigrationPreservesAccountsAndRelationships(t *testing.T) {
	store := uuidTestStore(t)
	ctx := context.Background()
	// The pre-UUID registration path creates sessions, recovery codes and membership.
	invite, err := store.Invite(ctx, "UUIDExisting")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.Register(ctx, "UUIDExisting", invite, "test-password-long-enough", "Test device")
	if err != nil {
		t.Fatal(err)
	}

	otherInvite, err := store.Invite(ctx, "UUIDExistingOther")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := store.Register(ctx, "UUIDExistingOther", otherInvite, "test-password-long-enough", "Other device")
	if err != nil {
		t.Fatal(err)
	}
	var legacyID int64
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username_key='uuidexisting'`).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertMessage(ctx, Identity{ID: legacyID, Username: "UUIDExisting"}, "preserved message"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Invite(ctx, "UUIDUnusedInvite"); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string][32]byte {
		result := map[string][32]byte{}
		for _, table := range []string{"users", "sessions", "recovery_codes", "conversation_members", "messages", "invites", "conversations"} {
			var data string
			sql := `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'account_uuid' ORDER BY (to_jsonb(t)-'account_uuid')::text),'[]'::jsonb)::text FROM ` + table + ` t`
			if err := store.db.QueryRowContext(ctx, sql).Scan(&data); err != nil {
				t.Fatal(err)
			}
			result[table] = sha256.Sum256([]byte(data))
		}
		return result
	}
	before := snapshot()
	var sequenceBefore int64
	var calledBefore bool
	if err := store.db.QueryRowContext(ctx, `SELECT last_value,is_called FROM users_id_seq`).Scan(&sequenceBefore, &calledBefore); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckSchema(ctx); err == nil {
		t.Fatal("new binary accepted pre-UUID schema")
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for table, hash := range snapshot() {
		if hash != before[table] {
			t.Errorf("migration changed preexisting %s data", table)
		}
	}
	var sequenceAfter int64
	var calledAfter bool
	if err := store.db.QueryRowContext(ctx, `SELECT last_value,is_called FROM users_id_seq`).Scan(&sequenceAfter, &calledAfter); err != nil || sequenceBefore != sequenceAfter || calledBefore != calledAfter {
		t.Fatal("legacy sequence changed")
	}
	actor, err := store.Authenticate(ctx, "UUIDExisting", token)
	if err != nil || actor.ID != legacyID || actor.UUID.IsZero() {
		t.Fatal("existing session or mapping lost")
	}
	otherActor, err := store.Authenticate(ctx, "UUIDExistingOther", otherToken)
	if err != nil || otherActor.UUID.IsZero() || otherActor.UUID == actor.UUID {
		t.Fatal("existing-account UUID mapping is not unique")
	}
	uuid := actor.UUID
	if uuid.bytes[6]>>4 != 4 || uuid.bytes[8]>>6 != 2 {
		t.Fatal("database did not generate UUIDv4")
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := store.Authenticate(ctx, "UUIDExisting", token)
	if err != nil || again.UUID != uuid {
		t.Fatal("repeated migration changed UUID mapping")
	}
	againOther, err := store.Authenticate(ctx, "UUIDExistingOther", otherToken)
	if err != nil || againOther.UUID != otherActor.UUID {
		t.Fatal("repeated migration changed another UUID mapping")
	}
	// Mimic the old binary's insert, which knows nothing about the new column.
	var newID int64
	var generated AccountUUID
	if err := store.db.QueryRowContext(ctx, `INSERT INTO users(username,username_key,password_hash) VALUES('UUIDDefault','uuiddefault','fixture') RETURNING id,account_uuid`).Scan(&newID, &generated); err != nil || newID <= otherActor.ID || generated.IsZero() || generated == uuid {
		t.Fatal("registration default or legacy sequence failed")
	}
	if generated.bytes[6]>>4 != 4 || generated.bytes[8]>>6 != 2 {
		t.Fatal("registration default is not UUIDv4")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE users SET account_uuid=$1 WHERE id=$2`, uuid, newID); err == nil {
		t.Fatal("duplicate UUID accepted")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE users SET account_uuid=NULL WHERE id=$1`, newID); err == nil {
		t.Fatal("NULL UUID accepted")
	}
	// All four account foreign keys remain BIGINT and still reference users.id.
	var references int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND data_type='bigint' AND (table_name,column_name) IN (('sessions','user_id'),('recovery_codes','user_id'),('conversation_members','user_id'),('messages','sender_id'))`).Scan(&references); err != nil || references != 4 {
		t.Fatal("integer foreign key columns changed")
	}
	var constraints int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE contype='f' AND confrelid='users'::regclass`).Scan(&constraints); err != nil || constraints != 4 {
		t.Fatal("existing account FK constraints changed")
	}
	if _, err := store.InsertMessage(ctx, actor, "post-migration chat"); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(ctx, "UUIDExisting", token)
	if err != nil || len(sessions) != 1 || !sessions[0].Current {
		t.Fatal("session metadata lost")
	}
	history, _, err := store.History(ctx, 0)
	if err != nil || len(history) != 2 || history[0].Text != "preserved message" {
		t.Fatal("message history lost")
	}
}

func TestUUIDMigrationAtomicity(t *testing.T) {
	store := uuidTestStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO users(username,username_key,password_hash) VALUES('Atomic','atomic','fixture')`); err != nil {
		t.Fatal(err)
	}
	// Force the final constraint statement to fail after column creation/backfill.
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE users ADD CONSTRAINT users_account_uuid_unique UNIQUE(username_key)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err == nil {
		t.Fatal("injected migration failure ignored")
	}
	var columns, ledger int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='users' AND column_name='account_uuid'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version='003_account_uuid.up.sql'`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if columns != 0 || ledger != 0 {
		t.Fatal("failed migration left a partial schema or ledger")
	}
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE users DROP CONSTRAINT users_account_uuid_unique`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var id AccountUUID
	if err := store.db.QueryRowContext(ctx, `SELECT account_uuid FROM users WHERE username_key='atomic'`).Scan(&id); err != nil || id.IsZero() {
		t.Fatal("retry lost existing account")
	}
}
