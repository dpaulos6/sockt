package transport

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebSocketStreamRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ServeWS(w, r, func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 128)
			n, err := c.Read(buf)
			if err != nil {
				t.Errorf("server read: %v", err)
				return
			}
			if _, err := c.Write(buf[:n]); err != nil {
				t.Errorf("server write: %v", err)
			}
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	conn, err := DialWS(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("{\"type\":\"ping\"}\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "{\"type\":\"ping\"}\n" {
		t.Fatalf("got %q", buf[:n])
	}
}
