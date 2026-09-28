package server

import (
	"strings"
	"sync"
	"time"
)

// Account-facing password and recovery operations are rate-limited per
// username. This is deliberately conservative for a small invite-only beta.
type attemptWindow struct {
	count int
	until time.Time
}
type attemptLimiter struct {
	mu    sync.Mutex
	users map[string]attemptWindow
}

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{users: make(map[string]attemptWindow)}
}

func (l *attemptLimiter) allow(username string) bool {
	now := time.Now()
	key := strings.ToLower(username)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.users) >= 4096 {
		for k, v := range l.users {
			if now.After(v.until) {
				delete(l.users, k)
			}
		}
		if _, exists := l.users[key]; !exists && len(l.users) >= 4096 {
			return false
		}
	}
	window, ok := l.users[key]
	if !ok || now.After(window.until) {
		window = attemptWindow{until: now.Add(10 * time.Minute)}
	}
	window.count++
	l.users[key] = window
	return window.count <= 10
}
func (l *attemptLimiter) reset(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.users, strings.ToLower(username))
}
