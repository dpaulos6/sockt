package config

import (
	"path/filepath"
	"testing"
)

func TestTokenReplacementPreservesLatestCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(Config{Token: "old", LastSeen: 2}); err != nil {
		t.Fatal(err)
	}
	s.UpdateSeen(19)
	if err = s.SetToken("new"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if c := reopened.Snapshot(); c.Token != "new" || c.LastSeen != 19 {
		t.Fatal("token write replaced latest cursor")
	}
	if err = s.SetToken(""); err != nil {
		t.Fatal(err)
	}
	if c := s.Snapshot(); c.Token != "" || c.LastSeen != 0 {
		t.Fatal("logout retained credentials")
	}
}

func TestTokenSaveFailureClearsInMemoryCredential(t *testing.T) {
	s := &Store{path: t.TempDir(), Value: Config{Token: "invalidated"}}
	if s.SetToken("replacement") == nil {
		t.Fatal("expected file replacement failure")
	}
	if s.Snapshot().Token != "" {
		t.Fatal("old credential still usable")
	}
}
