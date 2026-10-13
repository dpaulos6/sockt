package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestAdminInviteRevocationPostgreSQL(t *testing.T) {
	dsn := os.Getenv("SOCKT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SOCKT_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Isolate the test from other integration-test accounts.
	for _, name := range []string{"AdminRevokeGuest", "AdminUsedGuest", "AdminOtherGuest"} {
		t.Cleanup(func() {
			db.db.ExecContext(context.Background(), `DELETE FROM invites WHERE username_key=lower($1)`, name)
		})
	}
	first, err := db.Invite(ctx, "AdminRevokeGuest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Invite(ctx, "AdminRevokeGuest"); err != nil {
		t.Fatal(err)
	}
	used, err := db.Invite(ctx, "AdminUsedGuest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Invite(ctx, "AdminOtherGuest"); err != nil {
		t.Fatal(err)
	}
	// Mark consumed without creating an account; verifies revocation retains used rows.
	if _, err := db.db.ExecContext(ctx, `UPDATE invites SET used_at=now() WHERE username_key='adminusedguest'`); err != nil {
		t.Fatal(err)
	}
	if n, err := db.RevokeInvites(ctx, "ADMINREVOKEGUEST"); err != nil || n != 2 {
		t.Fatalf("revoke n=%d err=%v", n, err)
	}
	if _, _, err := db.Register(ctx, "AdminRevokeGuest", first, "a-valid-long-password", "Test"); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("revoked invite accepted: %v", err)
	}
	if n, err := db.RevokeInvites(ctx, "AdminRevokeGuest"); err != nil || n != 0 {
		t.Fatalf("repeat n=%d err=%v", n, err)
	}
	if n, err := db.RevokeInvites(ctx, "AdminUsedGuest"); err != nil || n != 0 {
		t.Fatalf("used n=%d err=%v", n, err)
	}
	if _, _, err := db.Register(ctx, "AdminUsedGuest", used, "a-valid-long-password", "Test"); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("consumed invite accepted: %v", err)
	}
	var remaining int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM invites WHERE username_key IN ('adminusedguest','adminotherguest')`).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("unrelated/used invites lost: %d %v", remaining, err)
	}
	if _, err := db.RevokeInvites(ctx, "bad username"); err == nil {
		t.Fatal("invalid username accepted")
	}
	if n, err := db.AccountCount(ctx); err != nil || n < 0 {
		t.Fatalf("count %d %v", n, err)
	}
}
