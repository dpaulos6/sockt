package updater

import (
	"bytes"
	"os"
	"testing"
)

// Invoked by the release regression suite with genuine linker-injected values.
func TestReleaseInjectedValues(t *testing.T) {
	want := os.Getenv("SOCKT_TEST_PUBLIC_KEY")
	if want == "" {
		t.Skip("release injection check requires a disposable test key")
	}
	if PublicKeyHex != want {
		t.Fatal("release verification key was not embedded")
	}
	if CurrentVersion != "0.10.0" {
		t.Fatalf("noncanonical client version: %q", CurrentVersion)
	}
	raw, err := os.ReadFile(os.Getenv("SOCKT_TEST_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(raw, PublicKeyHex); err != nil {
		t.Fatalf("Go signer's manifest rejected by client: %v", err)
	}
	tampered := bytes.Replace(raw, []byte("0.10.0"), []byte("0.10.1"), 1)
	if _, err := Verify(tampered, PublicKeyHex); err == nil {
		t.Fatal("tampered manifest accepted")
	}
}
