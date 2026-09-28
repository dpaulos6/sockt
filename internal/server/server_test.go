package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"sockt/internal/database"
	"sockt/internal/protocol"
)

type fakeStore struct {
	mu        sync.Mutex
	history   []protocol.Message
	failWrite bool
}

func (s *fakeStore) Authenticate(_ context.Context, name, token string) (database.Identity, error) {
	for i, account := range []struct{ name, token string }{{"Paulos", "a-token"}, {"Miguel", "b-token"}, {"Ana", "c-token"}} {
		if strings.EqualFold(name, account.name) && token == account.token {
			return database.Identity{ID: int64(i + 1), Username: account.name}, nil
		}
	}
	return database.Identity{}, database.ErrInvalidSession
}
func (s *fakeStore) Register(_ context.Context, user, invite, password, device string) (string, []string, error) {
	if user != "Paulos" || invite != "invitation" || len(password) < 12 {
		return "", nil, database.ErrInvalidInvite
	}
	return "a-new-session-token", []string{"CODE1", "CODE2", "CODE3", "CODE4", "CODE5", "CODE6", "CODE7", "CODE8"}, nil
}
func (s *fakeStore) PasswordLoginDevice(ctx context.Context, user, password, device string) (string, error) {
	return s.PasswordLogin(ctx, user, password)
}
func (s *fakeStore) GenerateRecoveryCodes(_ context.Context, user, token, password string) ([]string, error) {
	if user != "Paulos" || password != "correctpassword" {
		return nil, database.ErrInvalidCredentials
	}
	return []string{"CODE1", "CODE2", "CODE3", "CODE4", "CODE5", "CODE6", "CODE7", "CODE8"}, nil
}
func (s *fakeStore) ChangePassword(_ context.Context, user, token, oldPassword, newPassword, device string) (string, error) {
	if user != "Paulos" || oldPassword != "correctpassword" {
		return "", database.ErrInvalidCredentials
	}
	return "another-session-token", nil
}
func (s *fakeStore) RecoverPassword(_ context.Context, user, code, newPassword, device string) (string, []string, error) {
	if user != "Paulos" || code != "CODE1" {
		return "", nil, database.ErrInvalidRecoveryCode
	}
	return "another-session-token", []string{"NEW1", "NEW2", "NEW3", "NEW4", "NEW5", "NEW6", "NEW7", "NEW8"}, nil
}
func (s *fakeStore) ListSessions(_ context.Context, user, token string) ([]protocol.SessionInfo, error) {
	return []protocol.SessionInfo{{ID: strings.Repeat("a", 64), Device: "Test PC", Current: true}}, nil
}
func (s *fakeStore) RevokeSession(_ context.Context, user, token, target string) (bool, error) {
	return true, nil
}
func (s *fakeStore) Logout(_ context.Context, user, token string) error { return nil }
func (s *fakeStore) PasswordLogin(_ context.Context, user, password string) (string, error) {
	if user != "Paulos" || password != "correctpassword" {
		return "", database.ErrInvalidCredentials
	}
	return "a-new-session-token", nil
}
func (s *fakeStore) History(_ context.Context, since int64) ([]protocol.Message, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := int64(0)
	if len(s.history) > 0 {
		last = s.history[len(s.history)-1].ID
	}
	start := len(s.history) - protocol.HistoryLimit
	if start < 0 {
		start = 0
	}
	if since > 0 && since <= last {
		for i, m := range s.history {
			if m.ID > since {
				if i < start {
					start = i
				}
				break
			}
		}
	}
	return append([]protocol.Message(nil), s.history[start:]...), last, nil
}
func (s *fakeStore) InsertMessage(_ context.Context, user database.Identity, text string) (protocol.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWrite {
		return protocol.Message{}, errors.New("failed persistence")
	}
	id := int64(1)
	if len(s.history) > 0 {
		id = s.history[len(s.history)-1].ID + 1
	}
	m := protocol.Message{ID: id, Username: user.Username, Text: text, SentAt: time.Now().UTC()}
	s.history = append(s.history, m)
	return m, nil
}

type wire struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func launch(t *testing.T, store Store) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, log.New(io.Discard, "", 0))
	done := make(chan error, 1)
	go func() { done <- s.Serve(lis) }()
	return lis.Addr().String(), func() {
		lis.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}
func connect(t *testing.T, addr, name, token string) (*wire, []protocol.Message) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	w := &wire{conn: conn, scanner: protocol.NewScanner(conn)}
	if err := protocol.Write(conn, protocol.Packet{Type: "login", Version: protocol.Version, Username: name, Token: token}); err != nil {
		t.Fatal(err)
	}
	history := []protocol.Message{}
	for i := 0; i < 10; i++ {
		p := read(t, w)
		switch p.Type {
		case "history":
			history = append(history, p.Messages...)
		case "ready":
			return w, history
		case "error":
			t.Fatalf("unexpected auth error: %s", p.Text)
		default:
			t.Fatalf("unexpected handshake packet: %v", p.Type)
		}
	}
	t.Fatal("missing ready packet")
	return nil, nil
}
func read(t *testing.T, w *wire) protocol.Packet {
	t.Helper()
	_ = w.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	p, err := protocol.Read(w.scanner)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func readType(t *testing.T, w *wire, target string) protocol.Packet {
	t.Helper()
	for i := 0; i < 10; i++ {
		p := read(t, w)
		if p.Type == target {
			return p
		}
		if p.Type != "presence" {
			t.Fatalf("expected %s, got %+v", target, p)
		}
	}
	t.Fatal("too many presence packets")
	return protocol.Packet{}
}
func TestRealTimeBroadcastAndPersistenceOnRestart(t *testing.T) {
	store := &fakeStore{}
	addr, stop := launch(t, store)
	a, _ := connect(t, addr, "Paulos", "a-token")
	b, _ := connect(t, addr, "Miguel", "b-token")
	if err := protocol.Write(a.conn, protocol.Packet{Type: "chat", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*wire{a, b} {
		p := readType(t, w, "message")
		if p.Message == nil || p.Message.ID != 1 || p.Message.Username != "Paulos" || p.Message.Text != "hello" {
			t.Fatalf("wrong broadcast: %+v", p)
		}
	}
	stop()
	addr, stop = launch(t, store)
	defer stop()
	_, history := connect(t, addr, "Ana", "c-token")
	if len(history) != 1 || history[0].Text != "hello" {
		t.Fatalf("history missing after restart: %+v", history)
	}
}
func TestRejectImpersonationAndDontBroadcastFailedInsert(t *testing.T) {
	store := &fakeStore{}
	addr, stop := launch(t, store)
	defer stop()
	w, _ := connect(t, addr, "Paulos", "a-token")
	outsider, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer outsider.Close()
	ow := &wire{conn: outsider, scanner: protocol.NewScanner(outsider)}
	if err := protocol.Write(outsider, protocol.Packet{Type: "login", Version: protocol.Version, Username: "Miguel", Token: "a-token"}); err != nil {
		t.Fatal(err)
	}
	if p := read(t, ow); p.Type != "error" {
		t.Fatalf("impersonation accepted: %+v", p)
	}
	store.mu.Lock()
	store.failWrite = true
	store.mu.Unlock()
	if err := protocol.Write(w.conn, protocol.Packet{Type: "chat", Text: "should not persist"}); err != nil {
		t.Fatal(err)
	}
	if p := readType(t, w, "error"); p.Type != "error" {
		t.Fatalf("expected storage failure: %+v", p)
	}
	store.mu.Lock()
	count := len(store.history)
	store.mu.Unlock()
	if count != 0 {
		t.Fatal("failed persistence changed history")
	}
}
func TestRegistrationAndPasswordLoginHandshake(t *testing.T) {
	addr, stop := launch(t, &fakeStore{})
	defer stop()
	for _, p := range []protocol.Packet{
		{Type: "register", Username: "Paulos", Invite: "invitation", Password: "correctpassword"},
		{Type: "password_login", Username: "Paulos", Password: "correctpassword"},
	} {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if err = protocol.Write(c, protocol.Packet{Type: p.Type, Version: protocol.Version, Username: p.Username, Password: p.Password, Invite: p.Invite}); err != nil {
			t.Fatal(err)
		}
		response, err := protocol.Read(protocol.NewScanner(c))
		c.Close()
		if err != nil || response.Type != "authenticated" || response.Token == "" {
			t.Fatalf("handshake failed %s: %+v %v", p.Type, response, err)
		}
	}
}

func TestOneAccountCanConnectFromTwoDevices(t *testing.T) {
	addr, stop := launch(t, &fakeStore{})
	defer stop()
	first, _ := connect(t, addr, "Paulos", "a-token")
	second, _ := connect(t, addr, "Paulos", "a-token")
	if err := protocol.Write(first.conn, protocol.Packet{Type: "chat", Text: "two devices"}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*wire{first, second} {
		p := readType(t, w, "message")
		if p.Message == nil || p.Message.Text != "two devices" {
			t.Fatalf("device did not receive message: %+v", p)
		}
	}
}

func TestAccountOperationsUseOneShotConnections(t *testing.T) {
	addr, stop := launch(t, &fakeStore{})
	defer stop()
	cases := []struct {
		request protocol.Packet
		expect  string
	}{
		{protocol.Packet{Type: "register", Username: "Paulos", Password: "correctpassword", Invite: "invitation", DeviceLabel: "Windows PC"}, "authenticated"},
		{protocol.Packet{Type: "password_login", Username: "Paulos", Password: "correctpassword", DeviceLabel: "Linux device"}, "authenticated"},
		{protocol.Packet{Type: "account_sessions", Username: "Paulos", Token: "a-token"}, "sessions"},
		{protocol.Packet{Type: "account_codes", Username: "Paulos", Token: "a-token", Password: "correctpassword"}, "recovery_codes"},
		{protocol.Packet{Type: "recover", Username: "Paulos", RecoveryCode: "CODE1", NewPassword: "next-password-123", DeviceLabel: "Windows PC"}, "authenticated"},
	}
	for _, tt := range cases {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		tt.request.Version = protocol.Version
		if err = protocol.Write(c, tt.request); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		p, err := protocol.Read(protocol.NewScanner(c))
		c.Close()
		if err != nil || p.Type != tt.expect {
			t.Fatalf("operation %s: %+v %v", tt.request.Type, p, err)
		}
		if p.Type == "recovery_codes" && len(p.RecoveryCodes) != 8 {
			t.Fatal("missing recovery codes")
		}
	}
}
