package tui

import (
	"testing"
	"time"

	"sockt/internal/client"
	"sockt/internal/protocol"
)

func TestHistoryOnlyChangesForServerBroadcast(t *testing.T) {
	events := make(chan client.Event)
	m := Model{username: "Paulos", events: events, width: 80, height: 24}
	msg := protocol.Message{ID: 7, Username: "Paulos", Text: "hello", SentAt: time.Now()}

	next, _ := m.Update(client.Event{Packet: protocol.Packet{Type: "system", Text: "someone joined"}})
	m = next.(Model)
	if len(m.history) != 0 {
		t.Fatalf("system event must not be a chat row: %+v", m.history)
	}

	next, _ = m.Update(client.Event{Packet: protocol.Packet{Type: "message", Message: &msg}})
	m = next.(Model)
	if len(m.history) != 1 || m.history[0].ID != 7 {
		t.Fatalf("server broadcast should appear exactly once: %+v", m.history)
	}
}

func TestReconnectHistoryRemainsOrderedAndUnique(t *testing.T) {
	m := Model{width: 80, height: 24}
	for _, id := range []int64{1, 3, 2, 3} {
		m.addMessage(protocol.Message{ID: id, Username: "Paulos", Text: "test"})
	}
	if len(m.history) != 3 || m.history[0].ID != 1 || m.history[1].ID != 2 || m.history[2].ID != 3 {
		t.Fatalf("replayed history was duplicated or misordered: %+v", m.history)
	}
}
