// Package client manages independent socket read/write loops and reconnects.
package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sockt/internal/transport"
	"sync"
	"sync/atomic"
	"time"

	"sockt/internal/protocol"
)

var (
	ErrOffline   = errors.New("not connected; wait for reconnect")
	ErrQueueFull = errors.New("send queue full; please retry")
	ErrStopped   = errors.New("client has stopped")
)

type Event struct {
	Packet protocol.Packet
	Status string
	Online bool
	Reset  bool
}
type session struct {
	conn     net.Conn
	scanner  *bufio.Scanner
	outgoing chan protocol.Packet
	done     chan struct{}
}
type Client struct {
	addr, username, token string
	incoming              chan Event
	done                  chan struct{}
	stopped               chan struct{}
	once                  sync.Once
	mu                    sync.RWMutex
	current               *session
	lastSeen              atomic.Int64
	onSeen                func(int64)
}

func New(addr, username, token string, lastSeen int64, onSeen func(int64)) *Client {
	c := &Client{addr: addr, username: username, token: token, incoming: make(chan Event, 512), done: make(chan struct{}), stopped: make(chan struct{}), onSeen: onSeen}
	c.lastSeen.Store(lastSeen)
	go c.run()
	return c
}
func (c *Client) Incoming() <-chan Event { return c.incoming }

// CloseAndWait is used before replacing credentials, so an old reader cannot
// persist a history cursor after logout has cleared it.
func (c *Client) CloseAndWait() {
	c.Close()
	<-c.stopped
}
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.done)
		c.mu.Lock()
		if c.current != nil {
			_ = c.current.conn.Close()
		}
		c.current = nil
		c.mu.Unlock()
	})
}
func (c *Client) emit(e Event) bool {
	select {
	case c.incoming <- e:
		return true
	case <-c.done:
		return false
	}
}
func (c *Client) SendMessage(text string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return ErrOffline
	}
	select {
	case <-c.done:
		return ErrStopped
	case <-c.current.done:
		return ErrOffline
	case c.current.outgoing <- protocol.Packet{Type: "chat", Text: text}:
		return nil
	default:
		return ErrQueueFull
	}
}
func (c *Client) remember(id int64) {
	for {
		old := c.lastSeen.Load()
		if id <= old {
			return
		}
		if c.lastSeen.CompareAndSwap(old, id) {
			if c.onSeen != nil {
				c.onSeen(id)
			}
			return
		}
	}
}
func (c *Client) run() {
	defer close(c.stopped)
	defer close(c.incoming)
	backoff := time.Second
	for {
		select {
		case <-c.done:
			return
		default:
		}
		c.emit(Event{Status: "Connecting…", Online: false})
		s, history, reset, err := c.dial()
		if err != nil {
			var denied *deniedError
			if errors.As(err, &denied) {
				c.emit(Event{Status: "Login refused: " + err.Error() + " · run sockt login to renew your session", Online: false})
				return
			}
			c.emit(Event{Status: fmt.Sprintf("Offline · retrying in %s (%v)", backoff, err), Online: false})
			select {
			case <-c.done:
				return
			case <-time.After(backoff):
			}
			if backoff < 15*time.Second {
				backoff *= 2
				if backoff > 15*time.Second {
					backoff = 15 * time.Second
				}
			}
			continue
		}
		backoff = time.Second
		c.mu.Lock()
		select {
		case <-c.done:
			c.mu.Unlock()
			s.conn.Close()
			return
		default:
		}
		c.current = s
		c.mu.Unlock()
		if reset {
			c.lastSeen.Store(0)
			if c.onSeen != nil {
				c.onSeen(0)
			}
			c.emit(Event{Reset: true})
		}
		for _, batch := range history {
			if !c.emit(Event{Packet: batch}) {
				s.conn.Close()
				return
			}
			for _, m := range batch.Messages {
				c.remember(m.ID)
			}
		}
		c.emit(Event{Status: "Connected", Online: true})
		err = c.connected(s)
		_ = s.conn.Close()
		close(s.done)
		c.mu.Lock()
		if c.current == s {
			c.current = nil
		}
		c.mu.Unlock()
		if err != nil && !errors.Is(err, io.EOF) {
			c.emit(Event{Status: "Connection lost · reconnecting…", Online: false})
		} else {
			c.emit(Event{Status: "Disconnected · reconnecting…", Online: false})
		}
	}
}

type deniedError struct{ message string }

func (e *deniedError) Error() string { return e.message }
func (c *Client) dial() (*session, []protocol.Packet, bool, error) {
	conn, err := transport.DialWS(context.Background(), c.addr, 5*time.Second)
	if err != nil {
		return nil, nil, false, err
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	since := c.lastSeen.Load()
	err = protocol.Write(conn, protocol.Packet{Version: protocol.Version, Type: "login", Username: c.username, Token: c.token, SinceID: since})
	if err != nil {
		conn.Close()
		return nil, nil, false, err
	}
	scanner := protocol.NewScanner(conn)
	var history []protocol.Packet
	reset := false
	for {
		packet, err := protocol.Read(scanner)
		if err != nil {
			conn.Close()
			return nil, nil, false, err
		}
		switch packet.Type {
		case "error":
			conn.Close()
			return nil, nil, false, &deniedError{packet.Text}
		case "history":
			history = append(history, packet)
		case "ready":
			if packet.LastID < since {
				reset = true
			}
			_ = conn.SetDeadline(time.Time{})
			return &session{conn: conn, scanner: scanner, outgoing: make(chan protocol.Packet, 64), done: make(chan struct{})}, history, reset, nil
		default:
			conn.Close()
			return nil, nil, false, errors.New("unexpected login packet")
		}
	}
}
func (c *Client) connected(s *session) error {
	// The writer is the ONLY place that writes to the socket after login.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			var p protocol.Packet
			select {
			case <-s.done:
				return
			case p = <-s.outgoing:
			case <-ticker.C:
				p = protocol.Packet{Type: "ping"}
			}
			_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := protocol.Write(s.conn, p); err != nil {
				_ = s.conn.Close()
				return
			}
			_ = s.conn.SetWriteDeadline(time.Time{})
		}
	}()
	scanner := s.scanner
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		p, err := protocol.Read(scanner)
		if err != nil {
			return err
		}
		if p.Type == "pong" {
			continue
		}
		if p.Type == "message" && p.Message != nil {
			c.remember(p.Message.ID)
		}
		if !c.emit(Event{Packet: p}) {
			return ErrStopped
		}
	}
}
