package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func signed(t *testing.T, m Manifest) ([]byte, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := SigningBytes(m)
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = hex.EncodeToString(ed25519.Sign(priv, payload))
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b, hex.EncodeToString(pub)
}
func TestVerifySignedManifestAndTampering(t *testing.T) {
	m := Manifest{Version: "0.8.2", Artifacts: map[string]Asset{"linux/amd64": {URL: "https://example.com/sockt", SHA256: strings.Repeat("a", 64)}}}
	raw, pub := signed(t, m)
	parsed, err := Verify(raw, pub)
	if err != nil || parsed.Version != "0.8.2" {
		t.Fatalf("valid signature rejected: %v", err)
	}
	changed := strings.Replace(string(raw), "0.8.2", "0.8.3", 1)
	if _, err = Verify([]byte(changed), pub); err == nil {
		t.Fatal("tampered signed manifest was accepted")
	}
	if _, err = Verify(raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong public key was accepted")
	}
}
func TestVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"0.8.1", "0.8.0", true}, {"0.8.0", "0.8.0", false}, {"0.7.9", "0.8.0", false}, {"1.0.0", "0.9.9", true}}
	for _, tc := range cases {
		v, err := Newer(tc.a, tc.b)
		if err != nil || v != tc.want {
			t.Errorf("%v: %v / %v", tc, v, err)
		}
	}
	for _, v := range []string{"", "0.1", "0.1.1-alpha", "1.01.3", "-1.0.0"} {
		if _, err := VersionParts(v); err == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
func TestSignedManifestDownloadAndChecksum(t *testing.T) {
	bytes := []byte("fake executable byte contents")
	checksum := sha256.Sum256(bytes)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/updates/stable.json":
			http.Error(w, "not set", 500)
		default:
			w.Write(bytes)
		}
	}))
	defer server.Close()
	domain := strings.TrimPrefix(server.URL, "https://")
	m := Manifest{Version: "0.8.1", Artifacts: map[string]Asset{
		runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL + "/updates/app", SHA256: hex.EncodeToString(checksum[:])},
	}}
	raw, pub := signed(t, m)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/updates/stable.json" {
			w.Write(raw)
			return
		}
		w.Write(bytes)
	})
	offer, err := Check(context.Background(), "wss://"+domain+"/ws", "0.8.0", pub, server.Client())
	if err != nil || offer == nil {
		t.Fatalf("expected update: %+v, %v", offer, err)
	}
	dest := filepath.Join(t.TempDir(), "sockt")
	path, err := Download(context.Background(), *offer, dest, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(bytes) {
		t.Fatalf("bad download: %q / %v", got, err)
	}
	offer.Asset.SHA256 = strings.Repeat("0", 64)
	if _, err = Download(context.Background(), *offer, dest, server.Client()); err == nil {
		t.Fatal("accepted mismatched binary")
	}
}
