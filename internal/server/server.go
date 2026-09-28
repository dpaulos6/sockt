package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	attempts        *attemptLimiter
}

func New(store Store, logger *log.Logger) *Server {
	return &Server{hub: newHub(store, logger), logger: logger, pending: make(chan struct{}, 32), passwordWorkers: make(chan struct{}, 3), attempts: newAttemptLimiter()}
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
	// Normal chat authentication uses the session token, never a password.
	if p.Type == "login" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	valid := map[string]bool{
		"register": true, "password_login": true, "recover": true,
		"account_password": true, "account_codes": true,
		"account_sessions": true, "account_revoke": true, "account_logout": true,
	}
	if !valid[p.Type] || !protocol.ValidUsername(p.Username) {
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "invalid account request"})
		return database.Identity{}, false
	}
	if p.DeviceLabel != "" && !protocol.ValidDeviceLabel(p.DeviceLabel) {
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "invalid device name"})
		return database.Identity{}, false
	}
	// Password hashing and one-time recovery operations share a bounded pool.
	needsWorker := p.Type == "register" || p.Type == "password_login" || p.Type == "recover" || p.Type == "account_password" || p.Type == "account_codes"
	if needsWorker {
		if !s.attempts.allow(p.Username) {
			_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "too many attempts; try again later"})
			return database.Identity{}, false
		}
		select {
		case s.passwordWorkers <- struct{}{}:
			defer func() { <-s.passwordWorkers }()
		default:
			_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: "server busy; try again shortly"})
			return database.Identity{}, false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var response protocol.Packet
	var err error
	switch p.Type {
	case "register":
		var codes []string
		response.Token, codes, err = s.hub.store.Register(ctx, p.Username, p.Invite, p.Password, p.DeviceLabel)
		response.RecoveryCodes = codes
		response.Type = "authenticated"
	case "password_login":
		response.Token, err = s.hub.store.PasswordLoginDevice(ctx, p.Username, p.Password, p.DeviceLabel)
		response.Type = "authenticated"
	case "recover":
		response.Token, response.RecoveryCodes, err = s.hub.store.RecoverPassword(ctx, p.Username, p.RecoveryCode, p.NewPassword, p.DeviceLabel)
		response.Type = "authenticated"
	case "account_password":
		response.Token, err = s.hub.store.ChangePassword(ctx, p.Username, p.Token, p.Password, p.NewPassword, p.DeviceLabel)
		response.Type = "authenticated"
	case "account_codes":
		response.RecoveryCodes, err = s.hub.store.GenerateRecoveryCodes(ctx, p.Username, p.Token, p.Password)
		response.Type = "recovery_codes"
	case "account_sessions":
		response.Sessions, err = s.hub.store.ListSessions(ctx, p.Username, p.Token)
		response.Type = "sessions"
	case "account_revoke":
		var revoked bool
		revoked, err = s.hub.store.RevokeSession(ctx, p.Username, p.Token, p.SessionID)
		if err == nil && revoked {
			s.hub.evictSession(p.Username, p.SessionID)
		}
		response.Type = "ok"
	case "account_logout":
		err = s.hub.store.Logout(ctx, p.Username, p.Token)
		if err == nil {
			hash := sha256.Sum256([]byte(p.Token))
			s.hub.evictSession(p.Username, hex.EncodeToString(hash[:]))
		}
		response.Type = "ok"
	}
	if err != nil {
		message := "account operation failed"
		switch {
		case errors.Is(err, database.ErrInvalidCredentials):
			message = "invalid username or password"
		case errors.Is(err, database.ErrInvalidInvite):
			message = "invite invalid, used or for another username"
		case errors.Is(err, database.ErrInvalidSession):
			message = "session expired or invalid; run sockt login"
		case errors.Is(err, database.ErrInvalidRecoveryCode):
			message = "invalid or already used recovery code"
		case errors.Is(err, database.ErrUsernameUnavailable):
			message = "username already registered"
		case p.Type == "register" && (len(p.Password) < 12 || len(p.Password) > 128):
			message = "password must be 12-128 bytes"
		case (p.Type == "recover" || p.Type == "account_password") && (len(p.NewPassword) < 12 || len(p.NewPassword) > 128):
			message = "new password must be 12-128 bytes"
		default:
			s.logger.Printf("account operation failed: %v", err)
		}
		_ = protocol.Write(conn, protocol.Packet{Type: "error", Text: message})
		return database.Identity{}, false
	}
	if needsWorker {
		s.attempts.reset(p.Username)
	}
	if p.Type == "account_password" || p.Type == "recover" {
		s.hub.evictUser(p.Username)
	}
	_ = protocol.Write(conn, response)
	// The account connection closes after one response; it never joins chat.
	return database.Identity{}, false
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
	digest := sha256.Sum256([]byte(login.Token))
	c := &peer{identity: ident, key: strings.ToLower(ident.Username), tokenHash: hex.EncodeToString(digest[:]), conn: conn, outgoing: make(chan protocol.Packet, 256)}
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
		if packet.Type == "ping" || packet.Type == "chat" {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_, authErr := s.hub.store.Authenticate(ctx, ident.Username, login.Token)
			cancel()
			if authErr != nil {
				// The dedicated writer owns this connection now; closing it forces a clean re-login.
				return
			}
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
