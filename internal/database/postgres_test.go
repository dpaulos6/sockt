package database

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"sockt/internal/protocol"
)

func TestPasswordHashAndVerification(t *testing.T) {
	encoded, err := HashPassword("my-very-long-private-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(encoded, "my-very-long-private-password") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(encoded, "wrong-password") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("corrupted-hash", "my-very-long-private-password") {
		t.Fatal("corrupt hash accepted")
	}
}
func TestPostgreSQLIntegration(t *testing.T) {
	dsn := os.Getenv("SOCKT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SOCKT_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Use a disposable database. This migration integration test intentionally
	// assumes a new instance without pre-existing user messages.
	legacy := []protocol.Message{{ID: 81, Username: "OldUser", Text: "old message", SentAt: time.Now().UTC()}}
	data, _ := json.Marshal(legacy)
	file := filepath.Join(t.TempDir(), "messages.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	n, err := db.ImportJSON(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 imported message, got %d", n)
	}
	n, err = db.ImportJSON(ctx, file)
	if err != nil || n != 0 {
		t.Fatalf("import not idempotent: n=%d err=%v", n, err)
	}
	invite, err := db.Invite(ctx, "Paulos")
	if err != nil {
		t.Fatal(err)
	}
	session, codes, err := db.Register(ctx, "Paulos", invite, "strong-password-very-unique", "My desktop")
	if len(codes) != 8 {
		t.Fatalf("new account should receive eight recovery codes, got %d", len(codes))
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Register(ctx, "Paulos", invite, "strong-password-very-unique", "Another device"); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("reused invite accepted: %v", err)
	}
	who, err := db.Authenticate(ctx, "Paulos", session)
	if err != nil || who.Username != "Paulos" {
		t.Fatalf("session authentication failed: %v", err)
	}
	if _, err := db.Authenticate(ctx, "SomebodyElse", session); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("username spoof succeeded: %v", err)
	}
	if _, err := db.PasswordLogin(ctx, "Paulos", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad password accepted: %v", err)
	}
	renewed, err := db.PasswordLogin(ctx, "Paulos", "strong-password-very-unique")
	if err != nil || renewed == "" {
		t.Fatalf("password login failed: %v", err)
	}
	sent, err := db.InsertMessage(ctx, who, "hello from postgres")
	if err != nil || sent.ID <= 81 {
		t.Fatalf("insert or sequence failed: %+v %v", sent, err)
	}
	history, last, err := db.History(ctx, 0)
	if err != nil || last != sent.ID || len(history) != 2 {
		t.Fatalf("history failed: %+v last=%d err=%v", history, last, err)
	}
	// Existing user can rotate codes, then reset password using exactly one.
	rotated, err := db.GenerateRecoveryCodes(ctx, "Paulos", session, "strong-password-very-unique")
	if err != nil || len(rotated) != 8 {
		t.Fatalf("rotate recovery codes: %d %v", len(rotated), err)
	}
	if _, _, err = db.RecoverPassword(ctx, "Paulos", codes[0], "my-rotated-password-123", "Recovered"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("obsolete recovery code accepted: %v", err)
	}
	fresh, freshCodes, err := db.RecoverPassword(ctx, "Paulos", rotated[0], "my-new-password-12345", "Recovered")
	if err != nil || len(fresh) != 43 || len(freshCodes) != 8 {
		t.Fatalf("recover failed: %v", err)
	}
	if _, _, err = db.RecoverPassword(ctx, "Paulos", rotated[0], "my-new-password-12345", "Attack"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("reused code accepted: %v", err)
	}
	if _, _, err = db.RecoverPassword(ctx, "Paulos", rotated[1], "more-password-123", "Attacker"); !errors.Is(err, ErrInvalidRecoveryCode) {
		t.Fatalf("other pre-recovery codes remained usable: %v", err)
	}
	if _, err = db.Authenticate(ctx, "Paulos", session); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("old session survived password reset: %v", err)
	}
	if _, err = db.Authenticate(ctx, "Paulos", fresh); err != nil {
		t.Fatalf("recovered session invalid: %v", err)
	}
	if _, err = db.PasswordLoginDevice(ctx, "Paulos", "strong-password-very-unique", "Old password attempt"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still valid after recovery: %v", err)
	}
	oldSession, err := db.PasswordLoginDevice(ctx, "Paulos", "my-new-password-12345", "Phone")
	if err != nil {
		t.Fatalf("second session login: %v", err)
	}
	sessions, err := db.ListSessions(ctx, "Paulos", fresh)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("expected two sessions: %+v %v", sessions, err)
	}
	var other string
	for _, v := range sessions {
		if !v.Current {
			other = v.ID
		}
	}
	if other == "" {
		t.Fatal("missing second session ID")
	}
	if revoked, err := db.RevokeSession(ctx, "Paulos", fresh, other); err != nil || !revoked {
		t.Fatalf("revoke other session: %v %v", revoked, err)
	}
	if _, err = db.Authenticate(ctx, "Paulos", oldSession); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("revoked device authenticated: %v", err)
	}
	changed, err := db.ChangePassword(ctx, "Paulos", fresh, "my-new-password-12345", "another-long-password-123", "PC")
	if err != nil || changed == "" {
		t.Fatalf("password change: %v", err)
	}
	if _, err = db.Authenticate(ctx, "Paulos", fresh); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("prior session survived change: %v", err)
	}
	if err = db.Logout(ctx, "Paulos", changed); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Authenticate(ctx, "Paulos", changed); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("logout didn't revoke current session: %v", err)
	}
}
