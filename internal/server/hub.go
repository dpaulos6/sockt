package server

import (
	"context"
	"errors"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"sockt/internal/database"
	"sockt/internal/protocol"
)

const maxOnline = 32

type Store interface {
	Authenticate(context.Context, string, string) (database.Identity, error)
	Register(context.Context, string, string, string) (string, error)
	PasswordLogin(context.Context, string, string) (string, error)
	History(context.Context, int64) ([]protocol.Message, int64, error)
	InsertMessage(context.Context, database.Identity, string) (protocol.Message, error)
}

type peer struct {
	identity database.Identity
	key      string
	conn     net.Conn
	outgoing chan protocol.Packet
}

type hub struct {
	mu      sync.Mutex
	clients map[string]*peer
	store   Store
	logger  *log.Logger
}

func newHub(store Store, logger *log.Logger) *hub {
	return &hub{clients: make(map[string]*peer), store: store, logger: logger}
}
func (c *peer) writeLoop(h *hub) {
	for pkt := range c.outgoing {
		_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := protocol.Write(c.conn, pkt); err != nil {
			h.logger.Printf("write error user=%q: %v", c.identity.Username, err)
			h.leave(c)
			return
		}
		_ = c.conn.SetWriteDeadline(time.Time{})
	}
}
func (h *hub) detachLocked(c *peer) bool {
	if h.clients[c.key] != c {
		return false
	}
	delete(h.clients, c.key)
	close(c.outgoing)
	_ = c.conn.Close()
	h.logger.Printf("disconnect user=%q", c.identity.Username)
	return true
}
func (h *hub) sendLocked(c *peer, p protocol.Packet) {
	if h.clients[c.key] != c {
		return
	}
	select {
	case c.outgoing <- p:
	default:
		h.logger.Printf("slow client disconnected user=%q", c.identity.Username)
		h.detachLocked(c)
	}
}
func (h *hub) broadcastLocked(p protocol.Packet) {
	for _, c := range h.clients {
		h.sendLocked(c, p)
	}
}
func (h *hub) presenceLocked() {
	users := make([]string, 0, len(h.clients))
	for _, c := range h.clients {
		users = append(users, c.identity.Username)
	}
	sort.Strings(users)
	h.broadcastLocked(protocol.Packet{Type: "presence", Users: users})
}

// Hold the hub lock for the DB snapshot and registration so that broadcasts
// cannot slip between a history snapshot and the first live message.
func (h *hub) register(c *peer, since int64) ([]protocol.Message, int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.clients[c.key]; exists {
		return nil, 0, errors.New("this user is already online")
	}
	if len(h.clients) >= maxOnline {
		return nil, 0, errors.New("room is full")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	history, lastID, err := h.store.History(ctx, since)
	if err != nil {
		return nil, 0, err
	}
	h.clients[c.key] = c
	h.logger.Printf("connect user=%q remote=%s", c.identity.Username, c.conn.RemoteAddr())
	h.presenceLocked()
	return history, lastID, nil
}
func (h *hub) leave(c *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.detachLocked(c) {
		h.presenceLocked()
	}
}
func (h *hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients {
		h.detachLocked(c)
	}
}
func (h *hub) send(c *peer, p protocol.Packet) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendLocked(c, p)
}

// Serialize inserts with broadcasts to preserve the order all connected clients
// observe. For a small group this simple trade-off is preferable to a queue.
func (h *hub) publish(c *peer, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.key] != c {
		return errors.New("client is no longer connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	m, err := h.store.InsertMessage(ctx, c.identity, strings.TrimSpace(text))
	if err != nil {
		return err
	}
	h.logger.Printf("message id=%d user=%q bytes=%d", m.ID, m.Username, len(m.Text))
	h.broadcastLocked(protocol.Packet{Type: "message", Message: &m})
	return nil
}
