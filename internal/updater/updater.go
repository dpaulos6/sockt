// Package updater verifies signed Sockt release metadata and stages updates.
// HTTPS protects transport; Ed25519 signatures authenticate releases even if
// the download host is compromised. Signing keys must never ship with clients.
package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"sockt/internal/buildinfo"
)

// Inject PublicKeyHex when building distributable clients using -ldflags.
// Development builds deliberately disable auto-updates if no key is supplied.
var PublicKeyHex = ""
var CurrentVersion = buildinfo.Version

const MaxManifestBytes = 32 * 1024
const MaxBinaryBytes int64 = 100 << 20

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version   string           `json:"version"`
	Notes     string           `json:"notes,omitempty"`
	Artifacts map[string]Asset `json:"artifacts"`
	Signature string           `json:"signature,omitempty"`
}

type Offer struct {
	Version    string
	Notes      string
	Asset      Asset
	OriginHost string
}

// SigningBytes is canonical JSON with the signature field omitted. Go's
// JSON encoder sorts map keys, producing stable bytes for Ed25519.
func SigningBytes(m Manifest) ([]byte, error) {
	m.Signature = ""
	return json.Marshal(m)
}

func Verify(raw []byte, publicHex string) (Manifest, error) {
	var m Manifest
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return m, errors.New("update manifest is missing or too large")
	}
	pub, err := hex.DecodeString(strings.TrimSpace(publicHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return m, errors.New("invalid embedded release public key")
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("invalid update manifest: %w", err)
	}
	signature, err := hex.DecodeString(m.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return m, errors.New("unsigned update manifest")
	}
	signingBytes, err := SigningBytes(m)
	if err != nil {
		return m, err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), signingBytes, signature) {
		return m, errors.New("invalid update signature")
	}
	if _, err = VersionParts(m.Version); err != nil {
		return m, err
	}
	return m, nil
}

func VersionParts(version string) ([3]int, error) {
	var parts [3]int
	version = strings.TrimPrefix(version, "v")
	fields := strings.Split(version, ".")
	if len(fields) != 3 {
		return parts, fmt.Errorf("invalid release version %q", version)
	}
	for i, s := range fields {
		if s == "" || (len(s) > 1 && s[0] == '0') {
			return parts, fmt.Errorf("invalid release version %q", version)
		}
		for _, ch := range s {
			if ch < '0' || ch > '9' {
				return parts, fmt.Errorf("invalid release version %q", version)
			}
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return parts, err
		}
		parts[i] = n
	}
	return parts, nil
}

func Newer(remote, local string) (bool, error) {
	a, err := VersionParts(remote)
	if err != nil {
		return false, err
	}
	b, err := VersionParts(local)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

// ManifestURL is derived from the currently configured WSS host. Never
// transmit an account token to the update endpoint.
func ManifestURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if u.Scheme != "wss" || u.Host == "" || u.Path != "/ws" {
		return "", errors.New("automatic updates require a WSS server")
	}
	return "https://" + u.Host + "/updates/stable.json", nil
}

// restrictClient follows redirects only when HTTPS and host identity are retained.
// Neither credential data nor release bytes may be fetched over plaintext.
func restrictClient(client *http.Client, expectedHost string) *http.Client {
	copy := *client
	prior := copy.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, expectedHost) {
			return http.ErrUseLastResponse
		}
		if prior != nil {
			return prior(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &copy
}

// Check makes a bounded HTTPS request, authenticates the signed manifest, and
// filters releases for the current OS and architecture. No credentials sent.
func Check(ctx context.Context, server, version, pubKey string, hc *http.Client) (*Offer, error) {
	if pubKey == "" {
		return nil, nil
	}
	manifestURL, err := ManifestURL(server)
	if err != nil {
		return nil, nil
	} // Local ws:// development never fetches updates.
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	origin, err := url.Parse(manifestURL)
	if err != nil {
		return nil, err
	}
	hc = restrictClient(hc, origin.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	m, err := Verify(raw, pubKey)
	if err != nil {
		return nil, err
	}
	newer, err := Newer(m.Version, version)
	if err != nil || !newer {
		return nil, err
	}
	asset, ok := m.Artifacts[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return nil, nil
	}
	origin, err = url.Parse(manifestURL)
	if err != nil {
		return nil, err
	}
	assetURL, err := url.Parse(asset.URL)
	if err != nil || assetURL.Scheme != "https" || !strings.EqualFold(assetURL.Host, origin.Host) || assetURL.User != nil || assetURL.Fragment != "" {
		return nil, errors.New("update download must use HTTPS on the current server host")
	}
	hash, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(hash) != sha256.Size {
		return nil, errors.New("invalid release checksum")
	}
	return &Offer{Version: m.Version, Notes: m.Notes, Asset: asset, OriginHost: origin.Host}, nil
}

// Download only stages the binary. Installation happens after the TUI exits.
func Download(ctx context.Context, offer Offer, targetExecutable string, hc *http.Client) (string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Minute}
	}
	parsed, err := url.Parse(offer.Asset.URL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, offer.OriginHost) {
		return "", errors.New("untrusted update URL")
	}
	hash, err := hex.DecodeString(offer.Asset.SHA256)
	if err != nil || len(hash) != sha256.Size {
		return "", errors.New("invalid expected release checksum")
	}
	// A sibling temp file ensures the eventual rename stays on one filesystem.
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	f, err := os.CreateTemp(filepath.Dir(targetExecutable), ".sockt-update-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("cannot stage update next to Sockt: %w", err)
	}
	defer f.Close()
	staged := f.Name()
	valid := false
	defer func() {
		if !valid {
			_ = os.Remove(staged)
		}
	}()
	hc = restrictClient(hc, offer.OriginHost)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, offer.Asset.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	count, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, MaxBinaryBytes+1))
	if err != nil {
		return "", err
	}
	if count > MaxBinaryBytes {
		return "", errors.New("update binary too large")
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), hash) != 1 {
		return "", errors.New("update checksum mismatch; existing Sockt was not changed")
	}
	if err = f.Chmod(0700); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	valid = true
	return staged, nil
}
