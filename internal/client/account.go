package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sockt/internal/protocol"
	"sockt/internal/transport"
)

// requestAccount always opens a separate short-lived authenticated WSS connection.
// Passwords/recovery codes are never stored in the local Sockt configuration.
func requestAccount(addr string, packet protocol.Packet) (protocol.Packet, error) {
	conn, err := transport.DialWS(context.Background(), addr, 10*time.Second)
	if err != nil {
		return protocol.Packet{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	packet.Version = protocol.Version
	if err := protocol.Write(conn, packet); err != nil {
		return protocol.Packet{}, err
	}
	response, err := protocol.Read(protocol.NewScanner(conn))
	if err != nil {
		return protocol.Packet{}, err
	}
	if response.Type == "error" {
		return protocol.Packet{}, errors.New(response.Text)
	}
	return response, nil
}

func expectAuth(packet protocol.Packet) (string, error) {
	if packet.Type != "authenticated" || len(packet.Token) != 43 {
		return "", fmt.Errorf("unexpected account response: %s", packet.Type)
	}
	return packet.Token, nil
}

func RegisterWithRecovery(addr, username, invite, password, device string) (string, []string, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "register", Username: username, Invite: invite, Password: password, DeviceLabel: device})
	if err != nil {
		return "", nil, err
	}
	token, err := expectAuth(p)
	return token, p.RecoveryCodes, err
}

// Retained as a lightweight public helper for older client integrations.
func Register(addr, username, invite, password string) (string, error) {
	token, _, err := RegisterWithRecovery(addr, username, invite, password, "Legacy client")
	return token, err
}

func PasswordLoginDevice(addr, username, password, device string) (string, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "password_login", Username: username, Password: password, DeviceLabel: device})
	if err != nil {
		return "", err
	}
	return expectAuth(p)
}
func PasswordLogin(addr, username, password string) (string, error) {
	return PasswordLoginDevice(addr, username, password, "Legacy client")
}

func RecoverPassword(addr, username, code, newPassword, device string) (string, []string, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "recover", Username: username, RecoveryCode: code, NewPassword: newPassword, DeviceLabel: device})
	if err != nil {
		return "", nil, err
	}
	token, err := expectAuth(p)
	return token, p.RecoveryCodes, err
}
func ChangePassword(addr, username, token, oldPassword, newPassword, device string) (string, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "account_password", Username: username, Token: token, Password: oldPassword, NewPassword: newPassword, DeviceLabel: device})
	if err != nil {
		return "", err
	}
	return expectAuth(p)
}
func GenerateRecoveryCodes(addr, username, token, password string) ([]string, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "account_codes", Username: username, Token: token, Password: password})
	if err != nil {
		return nil, err
	}
	if p.Type != "recovery_codes" || len(p.RecoveryCodes) != 8 {
		return nil, fmt.Errorf("unexpected recovery response: %s", p.Type)
	}
	return p.RecoveryCodes, nil
}
func ListSessions(addr, username, token string) ([]protocol.SessionInfo, error) {
	p, err := requestAccount(addr, protocol.Packet{Type: "account_sessions", Username: username, Token: token})
	if err != nil {
		return nil, err
	}
	if p.Type != "sessions" {
		return nil, fmt.Errorf("unexpected sessions response: %s", p.Type)
	}
	return p.Sessions, nil
}
func RevokeSession(addr, username, token, target string) error {
	p, err := requestAccount(addr, protocol.Packet{Type: "account_revoke", Username: username, Token: token, SessionID: target})
	if err != nil {
		return err
	}
	if p.Type != "ok" {
		return fmt.Errorf("unexpected revoke response: %s", p.Type)
	}
	return nil
}
func Logout(addr, username, token string) error {
	p, err := requestAccount(addr, protocol.Packet{Type: "account_logout", Username: username, Token: token})
	if err != nil {
		return err
	}
	if p.Type != "ok" {
		return fmt.Errorf("unexpected logout response: %s", p.Type)
	}
	return nil
}
