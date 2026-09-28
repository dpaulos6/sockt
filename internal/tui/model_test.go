package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sockt/internal/client"
	"sockt/internal/protocol"
	"sockt/internal/updater"
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

func TestAccountShortcutRequestsMenu(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.Update(tea.KeyPressMsg{
		Code: 'p',
		Mod:  tea.ModCtrl,
	})
	if !next.(Model).AccountRequested() {
		t.Fatal("Ctrl+P didn't open account management")
	}
}

func TestSlashSuggestionsAndSmallTerminal(t *testing.T) {
	m := Model{composer: textinput.New(), width: 80, height: 24}
	m.composer.SetValue("/")
	matches, open := m.slashMenu()
	if !open || len(matches) != 4 {
		t.Fatalf("/ should suggest all commands: %+v, open=%v", matches, open)
	}
	if got := len(m.slashMenuRows(m.width)); got != 5 {
		t.Errorf("expected header and 4 suggestions, got %d", got)
	}
	m.height = 8
	if got := len(m.slashMenuRows(m.width)); got != 1 || m.bodyHeight() != 2 {
		t.Fatalf("small screen should reserve chat/composer: menu=%d, chat=%d", got, m.bodyHeight())
	}
	m.composer.SetValue("/test")
	matches, open = m.slashMenu()
	if !open || len(matches) != 0 {
		t.Errorf("unknown prefix should display empty suggestions, not close: %+v, open=%v", matches, open)
	}
}

func TestUnknownSlashNeverSendsChat(t *testing.T) {
	// A nil connection will panic if Update accidentally calls SendMessage.
	m := Model{composer: textinput.New(), width: 80, height: 24, online: true}
	m.composer.SetValue("/test")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := next.(Model)
	if got.composer.Value() != "/test" || !strings.Contains(got.status, "Unknown command") {
		t.Fatalf("unknown command was not rejected locally: status=%q, input=%q", got.status, got.composer.Value())
	}
	if _, open := got.slashMenu(); open {
		t.Fatal("an unknown command's suggestions should dismiss after pressing Enter")
	}
}

func TestTabCompletesThenEnterExecutes(t *testing.T) {
	m := Model{composer: textinput.New(), width: 80, height: 24, online: true}
	m.composer.SetValue("/he")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(Model)
	if got := m.composer.Value(); got != "/help" {
		t.Fatalf("Tab should complete /he to /help, got %q", got)
	}
	if m.help {
		t.Fatal("Tab should never execute the completed command")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.help || m.composer.Value() != "" {
		t.Fatal("Enter should execute completed /help and clear the composer")
	}
}

func TestEnterFirstCompletesPartialCommand(t *testing.T) {
	m := Model{composer: textinput.New(), width: 80, height: 24, online: true}
	m.composer.SetValue("/he")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.help || m.composer.Value() != "/help" {
		t.Fatal("first Enter on /he must complete /help without executing")
	}
}

func TestUpdateSlashUsesExistingOffer(t *testing.T) {
	m := Model{composer: textinput.New(), width: 80, height: 24, online: true}
	m.composer.SetValue("/update")
	m.updateOffer = &updater.Offer{Version: "0.9.1"}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := next.(Model)
	if got.updateStage != "confirm" {
		t.Fatalf("/update should open normal signed-updater confirmation, got %q", got.updateStage)
	}
}
