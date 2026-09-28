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
func (s *fakeStore) Register(_ context.Context, user, invite, password string) (string, error) {
	if user != "Paulos" || invite != "invitation" || len(password) < 12 {
		return "", database.ErrInvalidInvite
	}
	return "a-new-session-token", nil
}
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
