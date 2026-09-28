// Package transport adapts WebSockets to the stream interface used by the
// Sockt protocol. It is the only package that knows the WebSocket library.
package transport

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"sockt/internal/config"
)

// DialWS connects to a Sockt WebSocket endpoint. Its returned connection is
// a byte stream so the existing newline-delimited JSON protocol stays intact.
// Important: never bind the NetConn to the short-lived dial timeout context.
func DialWS(parent context.Context, endpoint string, timeout time.Duration) (net.Conn, error) {
	if err := config.ValidateServerAddress(endpoint); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ws, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		CompressionMode: websocket.CompressionDisabled,
		// A redirect could otherwise carry a future auth packet to a different
		// hostname or downgrade WSS. Require the exact configured endpoint.
		HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	})
	if err != nil {
		if response != nil {
			// coder/websocket closes the handshake body on errors.
			return nil, fmt.Errorf("WebSocket handshake HTTP %d: %w", response.StatusCode, err)
		}
		return nil, fmt.Errorf("WebSocket connection failed: %w", err)
	}
	// The caller owns the returned net.Conn and MUST Close it.
	// Frames are text so other WebSocket clients can inspect the JSON protocol.
	return websocket.NetConn(context.Background(), ws, websocket.MessageText), nil
}

// ServeWS upgrades one HTTP request and passes its bidirectional byte stream
// to the Sockt server. websocket.Accept performs default same-origin checks
// for browser callers; the terminal CLI does not send an Origin header.
func ServeWS(w http.ResponseWriter, r *http.Request, handle func(net.Conn)) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/ws" {
		http.NotFound(w, r)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	} // library writes the HTTP error response
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageText)
	handle(conn) // Owns conn until the client disconnects.
}
