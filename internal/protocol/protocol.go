// Package protocol defines bounded newline-delimited JSON messages. In v0.7,
// messages travel over a secure WebSocket transport (binary WS frames).
package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	Version         = 2
	MaxPacketBytes  = 256 * 1024
	MaxMessageBytes = 2048
	HistoryLimit    = 50
	HistoryBatch    = 40
)

var validUsername = regexp.MustCompile(`^[A-Za-z0-9_]{1,24}$`)

func ValidUsername(s string) bool { return validUsername.MatchString(s) }
func ValidDeviceLabel(s string) bool {
	if len(s) == 0 || len(s) > 64 || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

type Message struct {
	ID       int64     `json:"id"`
	Username string    `json:"username"`
	Text     string    `json:"text"`
	SentAt   time.Time `json:"sent_at"`
}

// Handshake: register/password_login/recover use one-shot account requests.
// account_sessions, account_revoke, account_logout, account_password and
// account_codes use a session token, with password re-confirmation where needed.
// Streaming chat uses login (username, session token, since_id), chat and ping.
// Server -> client: history (messages), ready, message, presence, pong, error.
// SessionInfo is public account metadata, never a credential.
type SessionInfo struct {
	ID         string    `json:"id"`
	Device     string    `json:"device"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

type Packet struct {
	Version       int           `json:"version,omitempty"`
	Type          string        `json:"type"`
	Username      string        `json:"username,omitempty"`
	Invite        string        `json:"invite,omitempty"`
	Password      string        `json:"password,omitempty"`
	NewPassword   string        `json:"new_password,omitempty"`
	RecoveryCode  string        `json:"recovery_code,omitempty"`
	RecoveryCodes []string      `json:"recovery_codes,omitempty"`
	DeviceLabel   string        `json:"device_label,omitempty"`
	SessionID     string        `json:"session_id,omitempty"`
	Sessions      []SessionInfo `json:"sessions,omitempty"`
	Token         string        `json:"token,omitempty"`
	Text          string        `json:"text,omitempty"`
	SinceID       int64         `json:"since_id,omitempty"`
	LastID        int64         `json:"last_id,omitempty"`
	Message       *Message      `json:"message,omitempty"`
	Messages      []Message     `json:"messages,omitempty"`
	Users         []string      `json:"users,omitempty"`
}

func NewScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), MaxPacketBytes)
	return s
}
func Read(s *bufio.Scanner) (Packet, error) {
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return Packet{}, err
		}
		return Packet{}, io.EOF
	}
	var p Packet
	err := json.Unmarshal(s.Bytes(), &p)
	return p, err
}

// A single dedicated writer must own each connection after authentication.
func Write(w io.Writer, p Packet) error { return json.NewEncoder(w).Encode(p) }
