package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const LocalURL = "ws://127.0.0.1:8080/ws"

// ValidateServerAddress accepts encrypted WebSocket URLs for any public or
// private host. Plain ws:// is only permitted on the same machine for local
// development; credentials must never traverse a public plaintext connection.
func ValidateServerAddress(address string) error {
	u, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("invalid WebSocket URL: %w", err)
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return fmt.Errorf("expected wss://host/ws (or ws://localhost:8080/ws for local development)")
	}
	if u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Path != "/ws" {
		return fmt.Errorf("expected a URL with /ws and no credentials, query or fragment")
	}
	if u.Scheme == "ws" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("unencrypted ws:// is only allowed on localhost; use wss:// for remote Sockt servers")
		}
	}
	return nil
}
