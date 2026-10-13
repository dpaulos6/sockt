package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sockt/internal/client"
	"sockt/internal/protocol"
)

func TestAdminVisibilityAndLocalDenial(t *testing.T) {
	m := Model{composer: textinput.New(), width: 80, height: 30, online: true, help: true}
	m.composer.SetValue("/admin")
	matches, _ := m.slashMenu()
	if len(matches) != 0 || strings.Contains(m.View().Content, "/admin  server") {
		t.Fatal("admin visible before capability")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.page != chatPage || !strings.Contains(m.status, "unavailable") {
		t.Fatal("local unauthorized admin invocation not rejected")
	}
	next, _ = m.Update(client.Event{Packet: protocol.Packet{Type: "ready", Admin: true}})
	m = next.(Model)
	m.slashDismissed = false
	matches, _ = m.slashMenu()
	if len(matches) != 1 || matches[0].Name != "/admin" || !strings.Contains(m.View().Content, "/admin  server") {
		t.Fatal("admin capability missing from commands/help")
	}
}

func TestAdminCapabilityReset(t *testing.T) {
	cases := []tea.Msg{
		closedMsg{},
		client.Event{Status: "Login refused", Online: false},
		client.Event{Status: "Disconnected", Online: false},
		client.Event{Packet: protocol.Packet{Type: "ready"}},
		accountResult{signedOut: true},
	}
	for _, event := range cases {
		m := Model{composer: textinput.New(), adminAllowed: true, page: administrationPage, admin: adminModel{invite: "secret"}, width: 80, height: 24}
		next, _ := m.Update(event)
		m = next.(Model)
		if m.adminAllowed || m.admin.invite != "" || m.page == administrationPage {
			t.Fatalf("capability survives %T", event)
		}
	}
}

func TestAdminPanelPreservesChatAndRequiresConfirmation(t *testing.T) {
	m := Model{composer: textinput.New(), adminAllowed: true, online: true, width: 80, height: 24, scroll: 3, history: []protocol.Message{{ID: 1, Text: "history"}}}
	m.composer.SetValue("draft")
	next, _ := m.openAdmin()
	m = next.(Model)
	next, _ = m.adminKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = next.(Model)
	m.admin.input.SetValue("Guest")
	next, _ = m.adminKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.admin.confirm || m.admin.busy {
		t.Fatal("mutation skipped confirmation")
	}
	next, _ = m.adminKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = next.(Model)
	if m.admin.confirm {
		t.Fatal("N did not cancel confirmation")
	}
	next, _ = m.adminKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	next, _ = m.adminKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.page != chatPage || m.composer.Value() != "draft" || m.scroll != 3 || len(m.history) != 1 {
		t.Fatal("panel changed chat state")
	}
	// Chat broadcasts continue to update history while the panel is open.
	next, _ = m.openAdmin()
	m = next.(Model)
	next, _ = m.Update(client.Event{Packet: protocol.Packet{Type: "message", Message: &protocol.Message{ID: 2, Text: "live"}}})
	m = next.(Model)
	if len(m.history) != 2 || m.page != administrationPage {
		t.Fatal("panel interrupted chat")
	}
	m.adminComplete(protocol.Packet{Type: "admin_result", Invite: "secret"})
	next, _ = m.adminKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.admin.invite != "" {
		t.Fatal("secret retained after closing panel")
	}
}
