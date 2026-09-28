package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sockt/internal/protocol"
	"sockt/internal/transport"
)

// This test uses a real in-process HTTP WebSocket upgrade and exercises
// the SAME authenticated hub/store flow as the original TCP tests.
func TestWebSocketLoginBroadcastAndHistory(t *testing.T) {
	store := &fakeStore{}
	srv := New(store, log.New(io.Discard, "", 0))
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		transport.ServeWS(w, r, srv.AcceptConnection)
	})
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	defer srv.Shutdown()
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	connectWS := func(username, token string) (*wire, []protocol.Message) {
		t.Helper()
		conn, err := transport.DialWS(context.Background(), url, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		w := &wire{conn: conn, scanner: protocol.NewScanner(conn)}
		received := []protocol.Message{}
		if err := protocol.Write(conn, protocol.Packet{
			Version: protocol.Version, Type: "login", Username: username, Token: token,
		}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			p := read(t, w)
			switch p.Type {
			case "history":
				received = append(received, p.Messages...)
			case "ready":
				return w, received
			case "error":
				t.Fatalf("login for %s failed: %s", username, p.Text)
			default:
				t.Fatalf("unexpected startup packet %q", p.Type)
			}
		}
		t.Fatal("no ready event")
		return nil, nil
	}

	a, _ := connectWS("Paulos", "a-token")
	b, _ := connectWS("Miguel", "b-token")
	// A message's canonical sender comes from authenticated identity.
	if err := protocol.Write(a.conn, protocol.Packet{Type: "chat", Username: "Ana", Text: "hello websocket"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*wire{a, b} {
		p := readType(t, c, "message")
		if p.Message == nil || p.Message.Username != "Paulos" || p.Message.Text != "hello websocket" || p.Message.ID != 1 {
			t.Fatalf("bad broadcast: %+v", p)
		}
	}
	// Newly connecting users get durable history from the same store.
	_, history := connectWS("Ana", "c-token")
	if len(history) != 1 || history[0].ID != 1 || history[0].Text != "hello websocket" || history[0].Username != "Paulos" {
		t.Fatalf("new WebSocket client did not receive persisted history: %+v", history)
	}
	store.mu.Lock()
	count := len(store.history)
	store.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected one persisted message, got %d", count)
	}
}

// Ensure an account handshake also works over WebSockets.
func TestWebSocketPasswordLogin(t *testing.T) {
	srv := New(&fakeStore{}, log.New(io.Discard, "", 0))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.ServeWS(w, r, srv.AcceptConnection)
	}))
	defer ts.Close()
	defer srv.Shutdown()
	conn, err := transport.DialWS(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := protocol.Write(conn, protocol.Packet{Version: protocol.Version, Type: "password_login", Username: "Paulos", Password: "correctpassword"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	p, err := protocol.Read(protocol.NewScanner(conn))
	if err != nil || p.Type != "authenticated" || p.Token == "" {
		t.Fatalf("password login over WS: %+v / %v", p, err)
	}
}
