package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyLoopbackConfigUpgrade(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"version":2,"server":"127.0.0.1:8080","username":"Paulos","token":"active-token","last_seen":42,"style":"classic"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if store.Value.Server != LocalURL || store.Value.Token != "active-token" || store.Value.LastSeen != 42 {
		t.Fatalf("legacy local upgrade lost session: %+v", store.Value)
	}
}
func TestLegacyRemoteRequiresSecureReconfiguration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"version":2,"server":"100.70.20.30:8080","username":"Paulos","token":"old-token","last_seen":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if store.Value.Server != "" || store.Value.Token != "" || store.Value.LastSeen != 0 {
		t.Fatalf("legacy remote config should require secure setup: %+v", store.Value)
	}
}
