package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sockt/internal/protocol"
)

type account struct {
	Username  string    `json:"username"`
	TokenHash string    `json:"token_hash"`
	CreatedAt time.Time `json:"created_at"`
}

// Invite writes an individual high-entropy token's SHA-256 hash to users.json.
// The raw token is returned just once; re-inviting a user rotates their token.
// Call from socktd invite (one administrator per server configuration).
func Invite(path, username string) (string, error) {
	if !protocol.ValidUsername(username) {
		return "", errors.New("username must have 1-24 letters, digits or underscores")
	}
	users, err := loadAccounts(path)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	key := strings.ToLower(username)
	// Keep the original display name on subsequent rotations.
	previous, existed := users[key]
	created := time.Now().UTC()
	if existed {
		username = previous.Username
		created = previous.CreatedAt
	}
	users[key] = account{Username: username, TokenHash: hex.EncodeToString(hash[:]), CreatedAt: created}
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return "", err
	}
	if err = atomicFile(path, append(data, '\n')); err != nil {
		return "", err
	}
	return token, nil
}
func loadAccounts(path string) (map[string]account, error) {
	users := make(map[string]account)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return users, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	if err = json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("decode accounts: %w", err)
	}
	if users == nil {
		users = make(map[string]account)
	}
	return users, nil
}

// A fresh file read lets invitations created while socktd is running work
// immediately. High-entropy tokens are hashed; passwords would need Argon2id.
func authenticate(path, username, token string) (string, bool) {
	if !protocol.ValidUsername(username) || len(token) != 43 {
		return "", false
	}
	users, err := loadAccounts(path)
	if err != nil {
		return "", false
	}
	user, ok := users[strings.ToLower(username)]
	if !ok {
		return "", false
	}
	got := sha256.Sum256([]byte(token))
	expected, err := hex.DecodeString(user.TokenHash)
	if err != nil || len(expected) != sha256.Size {
		return "", false
	}
	if subtle.ConstantTimeCompare(got[:], expected) != 1 {
		return "", false
	}
	return user.Username, true
}

// Atomic replace limits the chance of a crash producing a partially written
// JSON file. Same-directory temp files keep rename on a single filesystem.
func atomicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sockt-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	// On Windows, os.Rename may not replace an existing destination.
	// In that case fail safely; don't delete the last good JSON file.
	if err = os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
