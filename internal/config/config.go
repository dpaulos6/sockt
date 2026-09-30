// Package config stores one local Sockt user's connection profile.
// Treat config.json like a login credential: it contains a session token.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Version     int    `json:"version"`
	Server      string `json:"server"`
	Username    string `json:"username"`
	Token       string `json:"token"`
	LastSeen    int64  `json:"last_seen"`
	Style       string `json:"style"`
	Quiet       bool   `json:"quiet"`
	DeviceLabel string `json:"device_label,omitempty"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	Value Config
}

// Snapshot and SetToken serialize account changes with background history saves.
func (s *Store) Snapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Value
}

func (s *Store) SetToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.Value
	c.Token = token
	if token == "" {
		c.LastSeen = 0
	}
	if err := write(s.path, c); err != nil {
		// The server may already have invalidated the old token. Fail closed
		// in memory even when the credential file cannot be replaced.
		s.Value.Token = ""
		return err
	}
	s.Value = c
	return nil
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sockt", "config.json"), nil
}
func Open(path string) (*Store, error) {
	c := Config{Style: "minimal", Quiet: true, Server: DefaultRemoteURL}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{path: path, Value: c}, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if c.Version != 2 && c.Version != 3 {
		// Older invite tokens cannot be used as modern session tokens.
		c.Token = ""
		c.LastSeen = 0
	}
	// Upgrade local v0.6 connection settings. Never silently turn a former
	// private plaintext address into a public plaintext WebSocket endpoint.
	if c.Version == 2 {
		switch c.Server {
		case "localhost:8080":
			c.Server = "ws://localhost:8080/ws"
		case "127.0.0.1:8080":
			c.Server = LocalURL
		default:
			// Existing users can log in again after choosing a WSS URL.
			c.Server = ""
			c.Token = ""
			c.LastSeen = 0
		}
	}
	if c.Style == "" {
		c.Style = "minimal"
	}
	return &Store{path: path, Value: c}, nil
}
func (s *Store) Save(c Config) error {
	c.Version = 3
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := write(s.path, c); err != nil {
		return err
	}
	s.Value = c
	return nil
}
func (s *Store) UpdateSeen(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != 0 && id <= s.Value.LastSeen {
		return
	}
	s.Value.LastSeen = id
	if err := write(s.path, s.Value); err != nil {
		// Caller doesn't need the storage details; errors can be detected on
		// reconnect when history replays. Never log the credential-bearing file.
		_ = err
	}
}
func write(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sockt-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
