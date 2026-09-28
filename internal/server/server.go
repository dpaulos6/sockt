package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"sockt/internal/database"
	"sockt/internal/protocol"
)

type Server struct {
	hub             *hub
	logger          *log.Logger
	pending         chan struct{}
	passwordWorkers chan struct{}
}

func New(store Store, logger *log.Logger) *Server {
	return &Server{hub: newHub(store, logger), logger: logger, pending: make(chan struct{}, 32), passwordWorkers: make(chan struct{}, 3)}
}
func (s *Server) Shutdown() { s.hub.shutdown() }

// Serve supports legacy loopback tests and transport-independent server tests.
// Deployments use the WebSocket HTTP handler; no TCP endpoint is served by main.
func (s *Server) Serve(listener net.Listener) error {
	defer s.Shutdown()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go s.AcceptConnection(conn)
	}
}

// AcceptConnection handles exactly one already-established, stream-shaped
// connection. The WebSocket transport adapts its connection with NetConn.
// This method owns and closes conn; no caller should read from it afterward.
func (s *Server) AcceptConnection(conn net.Conn) {
	select {
	case s.pending <- struct{}{}:
		defer func() { <-s.pending }()
		s.handleClient(conn)
	default:
		_ = conn.Close()
	}
}

func (s *Server) authorize(conn net.Conn, p protocol.Packet) (database.Identity, bool) {
	// Password hashing is deliberately concurrency-limited for a small beta.
	if p.Type == "register" || p.Type == "password_login" {
		select {
		case s.passwordWorkers <- struct{}{}:
			defer func() { <-s.passwordWorkers }()
		default:
			_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "server busy; try again shortly"})
			return database.Identity{}, false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if !protocol.ValidUsername(p.Username) {
			_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "invalid username"})
			return database.Identity{}, false
		}
		var token string
		var err error
		switch p.Type {
		case "register":
			token, err = s.hub.store.Register(ctx, p.Username, p.Invite, p.Password)
		case "password_login":
			token, err = s.hub.store.PasswordLogin(ctx, p.Username, p.Password)
		}
		if err != nil {
			message := "registration or login failed"
			if errors.Is(err, database.ErrInvalidCredentials) {
				message = "invalid username or password"
			}
			if errors.Is(err, database.ErrInvalidInvite) {
				message = "invite invalid, used or for another username"
			}
			if errors.Is(err, database.ErrUsernameUnavailable) {
				message = "username already registered"
			}
			if p.Type == "register" && (len(p.Password) < 12 || len(p.Password) > 128) {
				message = "password must be 12-128 bytes"
			}
			if message == "registration or login failed" {
				s.logger.Printf("account operation failed: %v", err)
			}
			_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: message})
			return database.Identity{}, false
		}
		_ = protocol.Write(conn, protocol.Packet{Type: "authenticated", Username: p.Username, Token: token})
		return database.Identity{}, false // One-shot account handshake; close afterwards.
	}
	if p.Type != "login" {
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "expected login, register or password_login"})
		return database.Identity{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ident, err := s.hub.store.Authenticate(ctx, p.Username, p.Token)
	if err != nil {
		if !errors.Is(err, database.ErrInvalidSession) {
			s.logger.Printf("login database error: %v", err)
		}
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "session expired or invalid; run sockt login"})
		return database.Identity{}, false
	}
	return ident, true
}
func (s *Server) handleClient(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	scanner := protocol.NewScanner(conn)
	login, err := protocol.Read(scanner)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Printf("handshake read: %v", err)
		}
		return
	}
	if login.Version != protocol.Version {
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "incompatible Sockt version; update your client"})
		return
	}
	ident, valid := s.authorize(conn, login)
	if !valid {
		return
	}
	c := &peer{identity: ident, key: strings.ToLower(ident.Username), conn: conn, outgoing: make(chan protocol.Packet, 256)}
	history, lastID, err := s.hub.register(c, login.SinceID)
	if err != nil {
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "could not join room: " + err.Error()})
		return
	}
	defer s.hub.leave(c)
	if len(history) == 0 {
		if err = protocol.Write(conn, protocol.Packet{Type: "history", Messages: []protocol.Message{}}); err != nil {
			return
		}
	}
	for i := 0; i < len(history); i += protocol.HistoryBatch {
		end := i + protocol.HistoryBatch
		if end > len(history) {
			end = len(history)
		}
		if err = protocol.Write(conn, protocol.Packet{Type: "history", Messages: history[i:end]}); err != nil {
			return
		}
	}
	if err = protocol.Write(conn, protocol.Packet{Type: "ready", LastID: lastID}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	go c.writeLoop(s.hub)
	messages := 0
	window := time.Now()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		packet, readErr := protocol.Read(scanner)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				s.logger.Printf("read user=%q: %v", ident.Username, readErr)
			}
			return
		}
		switch packet.Type {
		case "ping":
			s.hub.send(c, protocol.Packet{Type: "pong"})
		case "chat":
			text := strings.TrimSpace(packet.Text)
			if len(text) == 0 || len([]byte(text)) > protocol.MaxMessageBytes {
				s.hub.send(c, protocol.Packet{Type: "error", Text: "message must be 1-2048 bytes"})
				continue
			}
			if time.Since(window) > 10*time.Second {
				window = time.Now()
				messages = 0
			}
			messages++
			if messages > 20 {
				s.hub.send(c, protocol.Packet{Type: "error", Text: "slow down; too many messages"})
				continue
			}
			if err := s.hub.publish(c, text); err != nil {
				s.logger.Printf("storage error: %v", err)
				s.hub.send(c, protocol.Packet{Type: "error", Text: "message could not be saved"})
			}
		default:
			s.hub.send(c, protocol.Packet{Type: "error", Text: "unknown request"})
		}
	}
}
