package main

import "testing"

func TestSafeListenRejectsPublicBinding(t *testing.T) {
	allowed := []string{
		"127.0.0.1:8080",
		"localhost:8080",
		"[::1]:8080",
	}

	rejected := []string{
		"0.0.0.0:8080",
		"8.8.8.8:8080",
		"[::]:8080",
		"100.80.20.11:8080",
		"192.168.0.1:8080",
	}

	for _, addr := range allowed {
		if err := safeListen(addr); err != nil {
			t.Errorf("expected %s to be accepted: %v", addr, err)
		}
	}

	for _, addr := range rejected {
		if err := safeListen(addr); err == nil {
			t.Errorf("expected %s to be rejected", addr)
		}
	}
}
