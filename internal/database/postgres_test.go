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
	session, err := db.Register(ctx, "Paulos", invite, "strong-password-very-unique")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Register(ctx, "Paulos", invite, "strong-password-very-unique"); !errors.Is(err, ErrInvalidInvite) {
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
}
