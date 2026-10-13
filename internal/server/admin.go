package server

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"sockt/internal/buildinfo"
	"sockt/internal/database"
	"sockt/internal/protocol"
)

// ParseAdminUserID accepts a canonical non-nil UUID. Empty disables admin.
func ParseAdminUserID(value string) (database.AccountUUID, error) {
	if value == "" {
		return database.AccountUUID{}, nil
	}
	id, err := database.ParseAccountUUID(value)
	if err != nil {
		return database.AccountUUID{}, errors.New("SOCKT_ADMIN_USER_ID must be a canonical lowercase non-nil UUID")
	}
	return id, nil
}

func NewWithAdmin(store Store, logger *log.Logger, value string) (*Server, error) {
	id, err := ParseAdminUserID(value)
	if err != nil {
		return nil, err
	}
	s := New(store, logger)
	s.adminUserID = id
	return s, nil
}

func (s *Server) isAdmin(identity database.Identity) bool {
	return !s.adminUserID.IsZero() && identity.UUID == s.adminUserID
}

type adminStore interface {
	AccountCount(context.Context) (int64, error)
	Invite(context.Context, string) (string, error)
	RevokeInvites(context.Context, string) (int64, error)
}

// Every admin request revalidates the server session, then checks its database ID.
// Request usernames, tokens, and capability flags cannot change the actor.
func (s *Server) adminRequest(session database.Identity, token string, p protocol.Packet) protocol.Packet {
	response := protocol.Packet{Type: "admin_error", Action: p.Type}
	mutation := p.Type == "admin_invite_create" || p.Type == "admin_invite_revoke"
	target := "invalid"
	if protocol.ValidUsername(p.Target) {
		target = strings.ToLower(p.Target)
	}
	outcome := "denied"
	defer func() {
		if mutation {
			s.logger.Printf("admin_audit actor_id=%s action=%s target=%q timestamp=%s outcome=%s", session.UUID, p.Type, target, time.Now().UTC().Format(time.RFC3339Nano), outcome)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	actor, err := s.hub.store.Authenticate(ctx, session.Username, token)
	if err != nil || actor.UUID != session.UUID || !s.isAdmin(actor) {
		response.Text = "Admin access denied"
		return response
	}
	store, ok := s.hub.store.(adminStore)
	if !ok {
		response.Text = "Admin service unavailable"
		outcome = "unavailable"
		return response
	}
	if mutation && (!p.Confirmed || !protocol.ValidUsername(p.Target)) {
		response.Text = "A valid username and confirmation are required"
		outcome = "invalid_request"
		return response
	}
	outcome = "failed"
	switch p.Type {
	case "admin_status":
		var count int64
		count, err = store.AccountCount(ctx)
		s.hub.mu.Lock()
		connections := len(s.hub.clients)
		s.hub.mu.Unlock()
		response.AdminStatus = &protocol.AdminStatus{Version: buildinfo.Version, Commit: buildinfo.Commit, UptimeSeconds: int64(time.Since(s.started).Seconds()), Connections: connections, Accounts: count}
	case "admin_invite_create":
		response.Invite, err = store.Invite(ctx, p.Target)
		response.Text = "Invite created. Save it securely; it is shown once."
	case "admin_invite_revoke":
		var count int64
		count, err = store.RevokeInvites(ctx, p.Target)
		response.Text = strconv.FormatInt(count, 10) + " unused invite(s) revoked"
	default:
		response.Text = "Unknown admin request"
		return response
	}
	if err != nil {
		// Database errors can include secrets. Never log or send them here.
		response.Invite = ""
		response.AdminStatus = nil
		response.Text = "Admin operation failed"
		if errors.Is(err, database.ErrUsernameUnavailable) {
			response.Text = "Username already registered"
		}
		return response
	}
	outcome = "success"
	response.Type = "admin_result"
	return response
}
