package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sockt/internal/protocol"
)

type jsonStore struct {
	path    string
	history []protocol.Message
	nextID  int64
}

func newJSONStore(path string) (*jsonStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	store := &jsonStore{path: path, history: []protocol.Message{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &store.history); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	if store.history == nil {
		store.history = []protocol.Message{}
	}
	for _, m := range store.history {
		if m.ID > store.nextID {
			store.nextID = m.ID
		}
	}
	return store, nil
}

// For a private beta, append+rewrite is acceptable; switch to SQLite for
// growing rooms. Only hub.mu-protected goroutines may access this store.
func (s *jsonStore) save(messages []protocol.Message) error {
	b, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return atomicFile(s.path, append(b, '\n'))
}
func (s *jsonStore) since(id int64) []protocol.Message {
	if len(s.history) == 0 {
		return nil
	}
	if id < 0 || id > s.nextID {
		id = 0
	}
	recentStart := len(s.history) - protocol.HistoryLimit
	if recentStart < 0 {
		recentStart = 0
	}
	if id == 0 {
		return append([]protocol.Message(nil), s.history[recentStart:]...)
	}
	// Replay at least the last 50 for a fresh terminal session, plus
	// everything missed since the last cursor if it is older than that.
	start := len(s.history)
	for i, m := range s.history {
		if m.ID > id {
			start = i
			break
		}
	}
	if recentStart < start {
		start = recentStart
	}
	return append([]protocol.Message(nil), s.history[start:]...)
}
