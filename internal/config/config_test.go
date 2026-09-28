package config

import "testing"

func TestWebSocketURLSafety(t *testing.T) {
	valid := []string{
		"wss://chat.example.com/ws", "wss://chat.example.com:443/ws",
		"ws://127.0.0.1:8080/ws", "ws://localhost:8080/ws", "ws://[::1]:8080/ws",
	}
	for _, u := range valid {
		if err := ValidateServerAddress(u); err != nil {
			t.Errorf("allow %s: %v", u, err)
		}
	}
	invalid := []string{
		"http://chat.example.com/ws", "ws://chat.example.com/ws", "ws://192.168.1.20:8080/ws",
		"ws://100.60.20.30:8080/ws", "wss://chat.example.com/chat", "wss://user:pass@chat.example.com/ws",
		"wss://chat.example.com/ws?token=abc", "127.0.0.1:8080", "wss:///ws", "ws://0.0.0.0:8080/ws",
	}
	for _, u := range invalid {
		if err := ValidateServerAddress(u); err == nil {
			t.Errorf("reject %s", u)
		}
	}
}
