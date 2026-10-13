package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"sockt/internal/database"
	"sockt/internal/protocol"
	"sockt/internal/transport"
)

// Run with a disposable DB and the unchanged previous release executable.
// No previous-server substitute or hand-written legacy queries count as validation.
func TestPreviousBinaryUUIDCompatibility(t *testing.T) {
	dsn := os.Getenv("SOCKT_TEST_DATABASE_URL")
	binary := os.Getenv("SOCKT_TEST_PREVIOUS_SERVER")
	expected := os.Getenv("SOCKT_TEST_PREVIOUS_VERSION")
	expectedSHA := os.Getenv("SOCKT_TEST_PREVIOUS_SHA256")
	if dsn == "" || binary == "" || expected == "" && expectedSHA == "" {
		t.Skip("requires disposable SOCKT_TEST_DATABASE_URL, SOCKT_TEST_PREVIOUS_SERVER, and exact version or SHA256")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("previous executable must use an absolute path")
	}

	if expectedSHA != "" {
		data, err := os.ReadFile(binary)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != expectedSHA {
			t.Fatal("previous executable checksum mismatch")
		}
		t.Logf("previous executable SHA256: %s", expectedSHA)
	}
	if expected != "" {
		cmd := exec.Command(binary, "version")
		cmd.Env = []string{} // Never inherit an operator's database credentials.
		version, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(version)) != expected {
			t.Fatal("previous executable provenance/version mismatch")
		}
		t.Logf("previous executable: %s", strings.TrimSpace(string(version)))
	}

	ctx := context.Background()
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open disposable database failed")
	}
	t.Cleanup(func() { base.Close() })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("uuid_previous_%x", random)
	if _, err := base.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
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
	dsn = parsed.String()
	old := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, args...)
		cmd.Env = []string{"DATABASE_URL=" + dsn, "SOCKT_ADMIN_USER_ID=" + adminUUID}
		return cmd
	}
	if err := old("migrate").Run(); err != nil {
		t.Fatal("previous binary migration failed")
	}
	store, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal("open isolated schema failed")
	}
	t.Cleanup(func() { store.Close() })
	// Reserve a loopback port briefly. Startup polling detects any bind failure.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	process := old("serve", "--listen", address)
	process.Stdout = io.Discard
	process.Stderr = io.Discard
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- process.Wait() }()
	t.Cleanup(func() {
		_ = process.Process.Signal(os.Interrupt)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			_ = process.Process.Kill()
			<-stopped
		}
	})
	httpClient := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(8 * time.Second)
	for {
		response, err := httpClient.Get("http://" + address + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("previous binary failed to start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	wsURL := "ws://" + address + "/ws"
	oneShot := func(p protocol.Packet) protocol.Packet {
		t.Helper()
		conn, err := transport.DialWS(ctx, wsURL, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
		p.Version = protocol.Version
		if err := protocol.Write(conn, p); err != nil {
			t.Fatal(err)
		}
		response, err := protocol.Read(protocol.NewScanner(conn))
		if err != nil {
			t.Fatal(err)
		}
		if response.Type == "error" {
			t.Fatal("previous binary account request failed")
		}
		return response
	}
	register := func(name string) string {
		t.Helper()
		invite, err := store.Invite(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		response := oneShot(protocol.Packet{Type: "register", Username: name, Invite: invite, Password: "previous-version-password", DeviceLabel: "Previous binary"})
		if response.Type != "authenticated" || len(response.Token) != 43 || len(response.RecoveryCodes) != 8 {
			t.Fatal("previous registration/session creation failed")
		}
		return response.Token
	}
	chat := func(name, token string) *wire {
		t.Helper()
		conn, err := transport.DialWS(ctx, wsURL, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		w := &wire{conn: conn, scanner: protocol.NewScanner(conn)}
		if err := protocol.Write(conn, protocol.Packet{Type: "login", Version: protocol.Version, Username: name, Token: token}); err != nil {
			t.Fatal(err)
		}
		for {
			p := read(t, w)
			if p.Type == "ready" {
				break
			}
			if p.Type != "history" {
				t.Fatal("previous authentication failed")
			}
		}
		return w
	}
	token := register("UUIDExistingAdmin")
	existing := chat("UUIDExistingAdmin", token)
	adminWireRequest(t, existing, protocol.Packet{Type: "chat", Text: "before migration"}, "message")
	sessionsBefore := oneShot(protocol.Packet{Type: "account_sessions", Username: "UUIDExistingAdmin", Token: token}).Sessions
	if len(sessionsBefore) != 1 {
		t.Fatal("pre-migration session missing")
	}
	// Migrate while the actual previous binary and its authenticated stream run.
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	actor, err := store.Authenticate(ctx, "UUIDExistingAdmin", token)
	if err != nil || actor.UUID.IsZero() {
		t.Fatal("backfilled account/session mapping invalid")
	}
	adminWireRequest(t, existing, protocol.Packet{Type: "chat", Text: "old connection after migration"}, "message")
	chat("UUIDExistingAdmin", token)
	newToken := register("UUIDOther")
	other, err := store.Authenticate(ctx, "UUIDOther", newToken)
	if err != nil || other.UUID.IsZero() || other.UUID == actor.UUID || other.ID == actor.ID {
		t.Fatal("previous registration UUID default failed")
	}
	newChat := chat("UUIDOther", newToken)
	adminWireRequest(t, newChat, protocol.Packet{Type: "chat", Text: "old registration after migration"}, "message")
	renewed := oneShot(protocol.Packet{Type: "password_login", Username: "UUIDExistingAdmin", Password: "previous-version-password", DeviceLabel: "New old-version session"}).Token
	if _, err := store.Authenticate(ctx, "UUIDExistingAdmin", renewed); err != nil {
		t.Fatal("previous password login/session creation failed")
	}
	chat("UUIDExistingAdmin", renewed)
	sessionsAfter := oneShot(protocol.Packet{Type: "account_sessions", Username: "UUIDExistingAdmin", Token: token}).Sessions
	if len(sessionsAfter) != 2 {
		t.Fatal("sessions not preserved")
	}
	found := false
	for _, session := range sessionsAfter {
		if session.ID == sessionsBefore[0].ID && session.Device == sessionsBefore[0].Device && session.ExpiresAt.Equal(sessionsBefore[0].ExpiresAt) {
			found = true
		}
	}
	if !found {
		t.Fatal("existing session metadata changed")
	}
	history, _, err := store.History(ctx, 0)
	if err != nil || len(history) != 3 {
		t.Fatal("previous-binary chat writes/history lost")
	}
	// The current server obtains UUID capabilities from the real session database.
	srv, err := NewWithAdmin(store, log.New(io.Discard, "", 0), actor.UUID.String())
	if err != nil {
		t.Fatal(err)
	}
	authorized, ready := adminWS(t, srv, "UUIDExistingAdmin", token)
	if !ready.Admin {
		t.Fatal("UUID admin capability missing")
	}
	unauthorized, ready := adminWS(t, srv, "UUIDOther", newToken)
	if ready.Admin {
		t.Fatal("other UUID gained capability")
	}
	for _, action := range []string{"admin_status", "admin_invite_create", "admin_invite_revoke"} {
		response := adminWireRequest(t, unauthorized, protocol.Packet{Type: action, Username: "UUIDExistingAdmin", Token: token, Admin: true, Confirmed: true, Target: "UUIDGuest"}, "admin_error")
		if response.Text != "Admin access denied" {
			t.Fatal("forged admin identity accepted")
		}
	}
	adminWireRequest(t, authorized, protocol.Packet{Type: "admin_status"}, "admin_result")
	created := adminWireRequest(t, authorized, protocol.Packet{Type: "admin_invite_create", Target: "UUIDGuest", Confirmed: true}, "admin_result")
	if len(created.Invite) != 43 {
		t.Fatal("authorized invite creation failed")
	}
	revoked := adminWireRequest(t, authorized, protocol.Packet{Type: "admin_invite_revoke", Target: "UUIDGuest", Confirmed: true}, "admin_result")
	if revoked.Text != "1 unused invite(s) revoked" {
		t.Fatal("authorized invite revocation failed")
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := store.Authenticate(ctx, "UUIDExistingAdmin", token)
	if err != nil || again.UUID != actor.UUID {
		t.Fatal("UUID mapping changed after repeat migration")
	}
}
