package server

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sockt/internal/database"
	"sockt/internal/protocol"
	"sockt/internal/transport"
)

const (
	adminUUID = "8f6cc9b8-15e2-4ed8-b3da-f615e419ed9a"
	otherUUID = "9b3a142c-53d6-4e1f-8d7b-2953c14a61bc"
	thirdUUID = "ceb309a7-91bb-4623-a25c-bd577793e775"
)

type adminFake struct {
	fakeStore
	adminMu         sync.Mutex
	calls           int
	invalidSession  bool
	replacementUUID database.AccountUUID
	fail            bool
}

func (s *adminFake) Authenticate(ctx context.Context, name, token string) (database.Identity, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	if s.invalidSession {
		return database.Identity{}, database.ErrInvalidSession
	}
	id, err := s.fakeStore.Authenticate(ctx, name, token)
	if !s.replacementUUID.IsZero() {
		id.UUID = s.replacementUUID
	}
	return id, err
}
func (s *adminFake) AccountCount(context.Context) (int64, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	s.calls++
	return 3, nil
}
func (s *adminFake) Invite(context.Context, string) (string, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	s.calls++
	if s.fail {
		return "", errors.New("postgres://private-db password-secret invite-secret")
	}
	return "invite-secret", nil
}
func (s *adminFake) RevokeInvites(context.Context, string) (int64, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	s.calls++
	return 2, nil
}

func TestAdminConfiguration(t *testing.T) {
	for _, value := range []string{"", adminUUID, otherUUID} {
		if _, err := NewWithAdmin(&fakeStore{}, log.Default(), value); err != nil {
			t.Errorf("valid %q: %v", value, err)
		}
	}
	for _, value := range []string{"0", "-1", "+1", " 1", "1 ", "1.0", "abc", "9223372036854775808", "\n", "１", "00000000-0000-0000-0000-000000000000", strings.ToUpper(adminUUID), " " + adminUUID, strings.ReplaceAll(adminUUID, "-", ""), adminUUID[:8] + "_" + adminUUID[9:]} {
		if _, err := NewWithAdmin(&fakeStore{}, log.Default(), value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	uuid, _ := database.ParseAccountUUID(adminUUID)
	if New(&fakeStore{}, log.Default()).isAdmin(database.Identity{ID: 1, UUID: uuid}) {
		t.Fatal("default grants admin")
	}
	configured, _ := NewWithAdmin(&fakeStore{}, log.Default(), adminUUID)
	if configured.isAdmin(database.Identity{ID: 1}) || !configured.isAdmin(database.Identity{ID: 99, UUID: uuid}) {
		t.Fatal("admin authorization depends on legacy ID")
	}
}

func adminWS(t *testing.T, srv *Server, username, token string) (*wire, protocol.Packet) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { transport.ServeWS(w, r, srv.AcceptConnection) }))
	t.Cleanup(ts.Close)
	t.Cleanup(srv.Shutdown)
	conn, err := transport.DialWS(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	w := &wire{conn: conn, scanner: protocol.NewScanner(conn)}
	if err := protocol.Write(conn, protocol.Packet{Type: "login", Version: protocol.Version, Username: username, Token: token, Admin: true}); err != nil {
		t.Fatal(err)
	}
	for {
		p := read(t, w)
		if p.Type == "ready" {
			return w, p
		}
		if p.Type != "history" {
			t.Fatalf("login: %+v", p)
		}
	}
}
func adminWireRequest(t *testing.T, w *wire, p protocol.Packet, want string) protocol.Packet {
	t.Helper()
	if err := protocol.Write(w.conn, p); err != nil {
		t.Fatal(err)
	}
	return readType(t, w, want)
}

func TestUnauthorizedAdminProtocolRequests(t *testing.T) {
	for _, config := range []string{"", adminUUID} {
		t.Run("config="+config, func(t *testing.T) {
			store := &adminFake{}
			srv, _ := NewWithAdmin(store, log.Default(), config)
			w, ready := adminWS(t, srv, "Miguel", "b-token")
			if ready.Admin {
				t.Fatal("unauthorized capability")
			}
			for _, action := range []string{"admin_status", "admin_invite_create", "admin_invite_revoke", "admin_unknown"} {
				p := adminWireRequest(t, w, protocol.Packet{Type: action, Username: "Paulos", Token: "a-token", Admin: true, Confirmed: true, Target: "Guest"}, "admin_error")
				if p.Text != "Admin access denied" || p.Invite != "" || p.AdminStatus != nil {
					t.Fatalf("unauthorized data: %+v", p)
				}
			}
			if store.calls != 0 {
				t.Fatal("unauthorized request reached database operation")
			}
			adminWireRequest(t, w, protocol.Packet{Type: "chat", Text: "still connected"}, "message")
		})
	}
	// Even the configured account has no capability with the setting unset.
	srv := New(&adminFake{}, log.Default())
	w, ready := adminWS(t, srv, "Paulos", "a-token")
	if ready.Admin {
		t.Fatal("unset config grants capability")
	}
	adminWireRequest(t, w, protocol.Packet{Type: "admin_status"}, "admin_error")
}

func TestAuthorizedAdminConfirmationAuditAndSessionRecheck(t *testing.T) {
	var audit bytes.Buffer
	store := &adminFake{}
	srv, _ := NewWithAdmin(store, log.New(&audit, "", 0), adminUUID)
	w, ready := adminWS(t, srv, "Paulos", "a-token")
	if !ready.Admin {
		t.Fatal("missing admin capability")
	}
	status := adminWireRequest(t, w, protocol.Packet{Type: "admin_status"}, "admin_result").AdminStatus
	if status == nil || status.Accounts != 3 || status.Connections != 1 || status.Version == "" || status.Commit == "" || status.UptimeSeconds < 0 {
		t.Fatalf("status: %+v", status)
	}
	for _, action := range []string{"admin_invite_create", "admin_invite_revoke"} {
		adminWireRequest(t, w, protocol.Packet{Type: action, Target: "Guest"}, "admin_error")
		adminWireRequest(t, w, protocol.Packet{Type: action, Target: "secret\npassword", Confirmed: true}, "admin_error")
	}
	if store.calls != 1 {
		t.Fatal("unconfirmed or invalid request mutated store")
	}
	p := adminWireRequest(t, w, protocol.Packet{Type: "admin_invite_create", Target: "Guest", Confirmed: true, Username: "Miguel"}, "admin_result")
	if p.Invite != "invite-secret" {
		t.Fatal("missing invite")
	}
	p = adminWireRequest(t, w, protocol.Packet{Type: "admin_invite_revoke", Target: "Guest", Confirmed: true}, "admin_result")
	if !strings.HasPrefix(p.Text, "2 ") {
		t.Fatalf("revoke: %+v", p)
	}
	store.adminMu.Lock()
	store.fail = true
	store.adminMu.Unlock()
	adminWireRequest(t, w, protocol.Packet{Type: "admin_invite_create", Target: "Guest", Confirmed: true}, "admin_error")
	for _, expected := range []string{"actor_id=" + adminUUID, "action=admin_invite_create", "action=admin_invite_revoke", "target=\"guest\"", "timestamp=", "outcome=success", "outcome=failed", "outcome=invalid_request"} {
		if !strings.Contains(audit.String(), expected) {
			t.Errorf("audit lacks %s", expected)
		}
	}
	for _, secret := range []string{"invite-secret", "password-secret", "private-db", "secret\npassword", "a-token"} {
		if strings.Contains(audit.String(), secret) {
			t.Errorf("audit leaked %q", secret)
		}
	}
	// Same username with a different database ID is not the admin account.
	store.adminMu.Lock()
	store.replacementUUID, _ = database.ParseAccountUUID(otherUUID)
	store.adminMu.Unlock()
	adminWireRequest(t, w, protocol.Packet{Type: "admin_status"}, "admin_error")
	store.adminMu.Lock()
	store.replacementUUID = database.AccountUUID{}
	store.invalidSession = true
	store.adminMu.Unlock()
	for _, action := range []string{"admin_status", "admin_invite_create", "admin_invite_revoke"} {
		adminWireRequest(t, w, protocol.Packet{Type: action, Target: "Guest", Confirmed: true}, "admin_error")
	}
	if store.calls != 4 {
		t.Fatalf("session recheck failed, calls=%d", store.calls)
	}
}
