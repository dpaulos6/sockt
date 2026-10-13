package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

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

// Exercise the actual serve startup path; malformed admin config must fail
// before DATABASE_URL validation or any database connection attempt.
func TestServeRejectsMalformedAdminConfiguration(t *testing.T) {
	if os.Getenv("SOCKT_STARTUP_TEST") == "1" {
		os.Args = []string{"socktd", "serve"}
		main()
		return
	}
	for _, value := range []string{"0", "-1", "not-an-id", " 1", "9223372036854775808", "00000000-0000-0000-0000-000000000000", "8F6CC9B8-15E2-4ED8-B3DA-F615E419ED9A"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestServeRejectsMalformedAdminConfiguration$")
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "SOCKT_ADMIN_USER_ID=") && !strings.HasPrefix(entry, "DATABASE_URL=") && !strings.HasPrefix(entry, "SOCKT_STARTUP_TEST=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "SOCKT_STARTUP_TEST=1", "SOCKT_ADMIN_USER_ID="+value, "DATABASE_URL=")
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "SOCKT_ADMIN_USER_ID must be a canonical lowercase non-nil UUID") {
			t.Fatalf("startup accepted malformed config %q: %s (%v)", value, output, err)
		}
	}
}
