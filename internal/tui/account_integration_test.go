package tui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"sockt/internal/client"
	"sockt/internal/config"
	"sockt/internal/protocol"
	"sockt/internal/transport"
)

// A disposable protocol peer: no environment credentials or database access.
func TestPersistentAccountWebSocketLifecycle(t *testing.T) {
	var logins atomic.Int32
	since := make(chan int64, 4)
	replacement := strings.Repeat("n", 43)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.ServeWS(w, r, func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(10 * time.Second))
			scanner := protocol.NewScanner(c)
			p, err := protocol.Read(scanner)
			if err != nil {
				return
			}
			switch p.Type {
			case "login":
				logins.Add(1)
				since <- p.SinceID
				_ = protocol.Write(c, protocol.Packet{Type: "ready", LastID: 8})
				_ = protocol.Write(c, protocol.Packet{Type: "message", Message: &protocol.Message{ID: 8, Text: "background"}})
				for {
					if _, err := protocol.Read(scanner); err != nil {
						return
					}
				}
			case "account_sessions":
				_ = protocol.Write(c, protocol.Packet{Type: "sessions", Sessions: []protocol.SessionInfo{{ID: "self", Device: "Test device", Current: true}}})
			case "account_password":
				_ = protocol.Write(c, protocol.Packet{Type: "authenticated", Token: replacement})
			case "account_logout":
				_ = protocol.Write(c, protocol.Packet{Type: "ok"})
			default:
				_ = protocol.Write(c, protocol.Packet{Type: "error", Text: "unsupported"})
			}
		})
	}))
	defer srv.Close()
	store, err := config.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	addr := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	if err := store.Save(config.Config{Server: addr, Username: "tester", Token: strings.Repeat("o", 43), LastSeen: 7}); err != nil {
		t.Fatal(err)
	}
	c := client.New(addr, "tester", store.Snapshot().Token, 7, store.UpdateSeen)
	m := New("tester", c, "minimal", false, addr).WithAccount(store)
	defer func() { m.Close() }()
	consume := func() {
		t.Helper()
		select {
		case e := <-m.events:
			n, _ := m.Update(e)
			m = n.(Model)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for test peer")
		}
	}
	for !m.online {
		consume()
	}
	m.composer.SetValue("draft")
	for i := 0; i < 20; i++ {
		m, _ = keyModel(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
		m, _ = keyModel(m, code(tea.KeyEscape))
	}
	m, _ = keyModel(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for len(m.history) == 0 {
		consume()
	}
	m, cmd := keyModel(m, code(tea.KeyEnter))
	m = finish(m, cmd)
	if logins.Load() != 1 || m.composer.Value() != "draft" || len(m.account.sessions) != 1 {
		t.Fatal("navigation replaced connection or state")
	}
	m.account.secret.values[0] = []byte("old-password")
	m.account.secret.values[1] = []byte("new-password-123")
	cmd = m.accountRequest("password")
	m = finish(m, cmd)
	for !m.online {
		consume()
	}
	if logins.Load() != 2 || store.Snapshot().Token != replacement || len(m.history) != 1 {
		t.Fatal("rotation failed")
	}
	if first, second := <-since, <-since; first != 7 || second != 8 {
		t.Fatal("history cursor reset on rotation")
	}
	cmd = m.accountRequest("logout")
	m = finish(m, cmd)
	if !m.account.signedOut || store.Snapshot().Token != "" || store.Snapshot().LastSeen != 0 {
		t.Fatal("logout did not clear credentials and cursor")
	}
}
