package client

import (
	"context"
	"errors"
	"fmt"
	"sockt/internal/transport"
	"time"

	"sockt/internal/protocol"
)

// Register and PasswordLogin are short, one-shot account handshakes.
// Normal chat continues to use the reconnecting Client in client.go.
func accountRequest(addr string, p protocol.Packet) (string, error) {
	conn, err := transport.DialWS(context.Background(), addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	p.Version = protocol.Version
	if err = protocol.Write(conn, p); err != nil {
		return "", err
	}
	reply, err := protocol.Read(protocol.NewScanner(conn))
	if err != nil {
		return "", err
	}
	if reply.Type == "error" {
		return "", errors.New(reply.Text)
	}
	if reply.Type != "authenticated" || len(reply.Token) != 43 {
		return "", fmt.Errorf("unexpected server response: %s", reply.Type)
	}
	return reply.Token, nil
}
func Register(addr, username, invite, password string) (string, error) {
	return accountRequest(addr, protocol.Packet{Type: "register", Username: username, Invite: invite, Password: password})
}
func PasswordLogin(addr, username, password string) (string, error) {
	return accountRequest(addr, protocol.Packet{Type: "password_login", Username: username, Password: password})
}
