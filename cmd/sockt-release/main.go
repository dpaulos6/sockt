// sockt-release creates offline Ed25519 keys and signs static release manifests.
// Store the private seed OUTSIDE the source tree, CI logs, and web root.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sockt/internal/updater"
)

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func validVersion(v string) bool {
	return regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(v)
}
func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: sockt-release keygen -out PRIVATE_KEY_FILE | sign -key PRIVATE_KEY_FILE -version 0.X.Y -base-url https://chat.dpaulos.pt/updates -dist dist -out dist/stable.json"))
	}
	switch os.Args[1] {
	case "keygen":
		flags := flag.NewFlagSet("keygen", flag.ExitOnError)
		out := flags.String("out", "", "path outside repository for release signing key")
		_ = flags.Parse(os.Args[2:])
		if *out == "" {
			fail(fmt.Errorf("-out is required"))
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			fail(err)
		}
		if _, err = fmt.Fprintln(f, hex.EncodeToString(priv.Seed())); err != nil {
			_ = f.Close()
			fail(err)
		}
		if err = f.Close(); err != nil {
			fail(err)
		}
		fmt.Println("Release PUBLIC key (safe to distribute):", hex.EncodeToString(pub))
		fmt.Println("Protect the private key; never place it in the repository or on the public VPS.")
	case "sign-checksums":
		signChecksums(os.Args[2:])
	case "check-key":
		flags := flag.NewFlagSet("check-key", flag.ExitOnError)
		key := flags.String("key", "", "private seed file")
		public := flags.String("public-key", "", "existing public key hex")
		_ = flags.Parse(os.Args[2:])
		b, err := os.ReadFile(*key)
		if err != nil {
			fail(err)
		}
		seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(seed) != ed25519.SeedSize {
			fail(fmt.Errorf("invalid private key"))
		}
		pub := hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
		if pub != strings.ToLower(*public) {
			fail(fmt.Errorf("signing key does not match existing public key"))
		}
	case "sign":
		flags := flag.NewFlagSet("sign", flag.ExitOnError)
		key := flags.String("key", "", "private signing key file")
		ver := flags.String("version", "", "release version (e.g. 0.8.1)")
		base := flags.String("base-url", "https://chat.dpaulos.pt/updates", "HTTPS release files directory")
		dist := flags.String("dist", "dist", "directory containing built release binaries")
		out := flags.String("out", "dist/stable.json", "signed manifest output path")
		notes := flags.String("notes", "", "short release notes")
		_ = flags.Parse(os.Args[2:])
		if *key == "" || !validVersion(*ver) {
			fail(fmt.Errorf("-key and -version are required"))
		}
		if _, err := updater.VersionParts(*ver); err != nil {
			fail(err)
		}
		*ver = strings.TrimPrefix(*ver, "v")
		u, err := url.Parse(*base)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			fail(fmt.Errorf("base URL must be HTTPS without credentials"))
		}
		b, err := os.ReadFile(*key)
		if err != nil {
			fail(err)
		}
		seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(seed) != ed25519.SeedSize {
			fail(fmt.Errorf("invalid signing key"))
		}
		priv := ed25519.NewKeyFromSeed(seed)
		m := updater.Manifest{Version: *ver, Notes: *notes, Artifacts: map[string]updater.Asset{}}
		for _, a := range []struct{ platform, file string }{
			{"windows/amd64", "sockt-windows-amd64.exe"},
			{"linux/amd64", "sockt-linux-amd64"},
			{"linux/arm64", "sockt-linux-arm64"},
		} {
			f, err := os.Open(filepath.Join(*dist, a.file))
			if err != nil {
				fail(fmt.Errorf("missing %s: %w", a.file, err))
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			_ = f.Close()
			if err != nil {
				fail(err)
			}
			m.Artifacts[a.platform] = updater.Asset{
				URL:    strings.TrimRight(*base, "/") + "/" + fmt.Sprintf("sockt-v%s-%s", *ver, a.file),
				SHA256: hex.EncodeToString(h.Sum(nil)),
			}
		}
		canonical, err := updater.SigningBytes(m)
		if err != nil {
			fail(err)
		}
		m.Signature = hex.EncodeToString(ed25519.Sign(priv, canonical))
		encoded, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fail(err)
		}
		if err = os.WriteFile(*out, append(encoded, '\n'), 0644); err != nil {
			fail(err)
		}
		fmt.Printf("Signed %s for release v%s; upload it only AFTER uploading all binaries.\n", *out, *ver)
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func signChecksums(args []string) {
	flags := flag.NewFlagSet("sign-checksums", flag.ExitOnError)
	key := flags.String("key", "", "private signing key file")
	dist := flags.String("dist", "dist", "directory containing release binaries")
	version := flags.String("version", "", "release version")
	out := flags.String("out", "dist/checksums.txt", "checksum output")
	_ = flags.Parse(args)
	if *key == "" || !validVersion(*version) {
		fail(fmt.Errorf("-key and -version are required"))
	}
	if _, err := updater.VersionParts(*version); err != nil {
		fail(err)
	}
	seedText, err := os.ReadFile(*key)
	if err != nil {
		fail(err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(seedText)))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail(fmt.Errorf("invalid signing key"))
	}
	files := []string{"sockt-windows-amd64.exe", "sockt-updater-windows-amd64.exe", "sockt-linux-amd64", "sockt-linux-arm64", "socktd-linux-amd64", "socktd-linux-arm64", "stable.json", "migration-plan", "release-meta", "install-sockt.ps1", "install-sockt.sh", "sockt-windows-amd64.zip"}
	var lines []string
	for _, name := range files {
		data, readErr := os.ReadFile(filepath.Join(*dist, name))
		if readErr != nil {
			fail(readErr)
		}
		sum := sha256.Sum256(data)
		lines = append(lines, hex.EncodeToString(sum[:])+"  "+name)
	}
	payload := []byte(strings.Join(lines, "\n") + "\n")
	if err = os.WriteFile(*out, payload, 0644); err != nil {
		fail(err)
	}
	// Detached checksum signatures are raw 64-byte Ed25519 signatures. Manifest
	// JSON retains its existing hex encoding for installed-client compatibility.
	if err = os.WriteFile(*out+".sig", ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload), 0644); err != nil {
		fail(err)
	}
	fmt.Println("Signed release checksums:", *out)
}
