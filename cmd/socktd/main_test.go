package main

import "testing"

func TestSafeListenRejectsPublicBinding(t *testing.T) {
	for _, a := range []string{"127.0.0.1:8080", "100.80.20.11:8080", "192.168.0.1:8080"} {
		if err := safeListen(a); err != nil {
			t.Errorf("expected permitted local address %s: %v", a, err)
		}
	}
	for _, a := range []string{"0.0.0.0:8080", "8.8.8.8:8080", "[::]:8080"} {
		if err := safeListen(a); err == nil {
			t.Errorf("public/unspecified bind accepted: %s", a)
		}
	}
}
