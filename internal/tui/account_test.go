package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sockt/internal/client"
	"sockt/internal/protocol"
	"sockt/internal/updater"
)

type fakeAccount struct {
	calls        []string
	err, saveErr error
	saved        string
	conn         *client.Client
}

func (f *fakeAccount) Sessions() ([]protocol.SessionInfo, error) {
	f.calls = append(f.calls, "sessions")
	return []protocol.SessionInfo{{ID: "other", Device: "Laptop"}, {ID: "self", Current: true}}, f.err
}
func (f *fakeAccount) Revoke(id string) error { f.calls = append(f.calls, id); return f.err }
func (f *fakeAccount) Password(old, next string) (string, error) {
	f.calls = append(f.calls, "password")
	if old != "old-password" || next != "new-password-123" {
		return "", errors.New("invalid test input")
	}
	return "replacement", f.err
}
func (f *fakeAccount) Codes(string) ([]string, error) {
	f.calls = append(f.calls, "codes")
	return []string{"private-code"}, f.err
}
func (f *fakeAccount) Logout() error            { f.calls = append(f.calls, "logout"); return f.err }
func (f *fakeAccount) SaveToken(s string) error { f.saved = s; return f.saveErr }
func (f *fakeAccount) Connect() *client.Client  { f.conn = &client.Client{}; return f.conn }
func accountTestModel() (Model, *fakeAccount) {
	f := &fakeAccount{}
	return Model{composer: textinput.New(), page: accountPage, accountService: f, width: 80, height: 24, online: true}, f
}
func keyModel(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) { n, c := m.Update(k); return n.(Model), c }
func code(k rune) tea.KeyPressMsg                          { return tea.KeyPressMsg{Code: k} }
func finish(m Model, c tea.Cmd) Model                      { n, _ := m.Update(c()); return n.(Model) }

func TestPersistentNavigation(t *testing.T) {
	m, _ := accountTestModel()
	m.page = chatPage
	m.scroll = 4
	m.composer.SetValue("unsent draft")
	conn := m.connection
	events := m.events
	for i := 0; i < 50; i++ {
		var cmd tea.Cmd
		m, cmd = keyModel(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
		if cmd != nil || m.page != accountPage {
			t.Fatal("navigation issued work or failed")
		}
		m, _ = keyModel(m, code(tea.KeyEscape))
	}
	if m.page != chatPage || m.scroll != 4 || m.composer.Value() != "unsent draft" || m.connection != conn || m.events != events {
		t.Fatal("navigation changed chat state")
	}
	m.composer.SetValue("/account")
	m, _ = keyModel(m, code(tea.KeyEnter))
	if m.page != accountPage || m.composer.Value() != "" {
		t.Fatal("slash navigation failed")
	}
}
func TestAccountBackgroundEventsAndResize(t *testing.T) {
	m, _ := accountTestModel()
	m.composer.SetValue("draft")
	m.scroll = 2
	n, _ := m.Update(client.Event{Packet: protocol.Packet{Type: "message", Message: &protocol.Message{ID: 1, Text: "background"}}})
	m = n.(Model)
	n, _ = m.Update(updateCheckMsg{offer: &updater.Offer{Version: "99"}})
	m = n.(Model)
	n, _ = m.Update(tea.WindowSizeMsg{Width: 22, Height: 8})
	m = n.(Model)
	if len(m.history) != 1 || m.updateOffer == nil || m.scroll < 2 || m.composer.Value() != "draft" {
		t.Fatal("background state lost")
	}
	if !m.View().AltScreen {
		t.Fatal("account must use alternate screen")
	}
}
func TestCredentialBoundaryAndCancellation(t *testing.T) {
	m, _ := accountTestModel()
	m.account.screen = passwordEntry
	m.composer.SetValue("draft")
	m, _ = keyModel(m, tea.KeyPressMsg{Code: 's', Text: "secret"})
	buf := m.account.secret.values[0]
	if strings.Contains(m.View().Content, "secret") || m.composer.Value() != "draft" {
		t.Fatal("credential escaped")
	}
	n, _ := m.Update(tea.PasteMsg{Content: "pasted secret\n/exit"})
	m = n.(Model)
	if string(m.account.secret.values[0]) != "secret" {
		t.Fatal("paste reached secret input")
	}
	m, _ = keyModel(m, code(tea.KeyEscape))
	if m.page != chatPage || len(m.account.secret.values[0]) != 0 {
		t.Fatal("cancel did not clear")
	}
	for _, b := range buf {
		if b != 0 {
			t.Fatal("buffer not wiped")
		}
	}
}
func TestPasswordValidationAndRotation(t *testing.T) {
	m, f := accountTestModel()
	m.account.screen = passwordEntry
	m.account.secret.values[0] = []byte("old-password")
	m.account.secret.field = 1
	m.account.secret.values[1] = []byte("short")
	m, c := keyModel(m, code(tea.KeyEnter))
	if c != nil || m.account.secret.field != 1 {
		t.Fatal("short password accepted")
	}
	m.account.secret.values[1] = []byte("new-password-123")
	m.account.secret.values[2] = []byte("new-password-123")
	m.account.secret.field = 2
	old := m.account.secret.values[0]
	m, c = keyModel(m, code(tea.KeyEnter))
	if !m.account.busy || c == nil || m.account.secret.values[0] != nil {
		t.Fatal("secrets not transferred")
	}
	m = finish(m, c)
	if f.saved != "replacement" || m.connection != f.conn || m.account.busy {
		t.Fatal("rotation failed")
	}
	for _, b := range old {
		if b != 0 {
			t.Fatal("command buffer retained")
		}
	}
}
func TestSessionsConfirmationAndDuplicateSubmission(t *testing.T) {
	m, f := accountTestModel()
	m, c := keyModel(m, code(tea.KeyEnter))
	_, duplicate := keyModel(m, code(tea.KeyEnter))
	if duplicate != nil {
		t.Fatal("duplicate request")
	}
	m = finish(m, c)
	if m.account.screen != devices || len(m.account.sessions) != 2 {
		t.Fatal("no devices")
	}
	m, c = keyModel(m, code(tea.KeyEnter))
	if c != nil || m.account.screen != confirmation || len(f.calls) != 1 {
		t.Fatal("revoked without confirmation")
	}
	m, c = keyModel(m, code(tea.KeyEnter))
	m = finish(m, c)
	if len(f.calls) != 2 || f.calls[1] != "other" || m.account.signedOut {
		t.Fatal("wrong revoke")
	}
}
func TestRecoveryAcknowledgment(t *testing.T) {
	m, _ := accountTestModel()
	m.account.screen = codesEntry
	m, c := keyModel(m, code(tea.KeyEnter))
	m = finish(m, c)
	codes := m.account.codes
	for _, k := range []tea.KeyPressMsg{code(tea.KeyEscape), {Code: 'c', Mod: tea.ModCtrl}, {Code: 'p', Mod: tea.ModCtrl}, code(tea.KeyF2)} {
		m, c = keyModel(m, k)
		if c != nil || m.account.screen != recoveryDisplay {
			t.Fatal("codes lost before acknowledgment")
		}
	}
	m, _ = keyModel(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.account.codes != nil || codes[0] != "" {
		t.Fatal("codes retained")
	}
}
func TestLogoutCurrentRevokeAndFailures(t *testing.T) {
	for _, action := range []string{"logout", "revoke", "password"} {
		for _, failure := range []string{"", "network", "storage"} {
			t.Run(action+failure, func(t *testing.T) {
				m, f := accountTestModel()
				m.account.target = protocol.SessionInfo{ID: "self", Current: true}
				m.account.secret.values[0] = []byte("old-password")
				m.account.secret.values[1] = []byte("new-password-123")
				if failure == "network" {
					f.err = errors.New("secret must not be shown")
				}
				if failure == "storage" {
					f.saveErr = errors.New("disk full")
				}
				c := m.accountRequest(action)
				m = finish(m, c)
				if strings.Contains(m.View().Content, "secret must") {
					t.Fatal("raw error leaked")
				}
				if failure == "network" && m.account.signedOut {
					t.Fatal("network failure signed out")
				}
				if failure != "network" && action != "password" && !m.account.signedOut {
					t.Fatal("current session remained active")
				}
				if failure == "storage" && !m.account.signedOut {
					t.Fatal("unsafe save failure")
				}
			})
		}
	}
}
func TestStaleConnectionEventsIgnored(t *testing.T) {
	m, _ := accountTestModel()
	m.events = make(chan client.Event)
	old := make(chan client.Event)
	n, _ := m.Update(networkMsg{events: old, closed: true})
	if !n.(Model).online {
		t.Fatal("stale close changed connection")
	}
}

func TestNarrowAccountSelectionAndRecoveryHint(t *testing.T) {
	m, _ := accountTestModel()
	m.width = 20
	m.height = 8
	m.account.selected = 3
	if !strings.Contains(m.View().Content, "Log out") {
		t.Fatal("selected row cropped")
	}
	m.account.screen = recoveryDisplay
	m.account.codes = []string{"one", "two", "three", "four", "five", "six", "seven", "eight"}
	if !strings.Contains(m.View().Content, "A saved") {
		t.Fatal("acknowledgment key hidden")
	}
	for i := 0; i < 7; i++ {
		m, _ = keyModel(m, code(tea.KeyDown))
	}
	if !strings.Contains(m.View().Content, "eight") {
		t.Fatal("last code unreachable")
	}
}

func TestPasswordMismatchAndInterruptClear(t *testing.T) {
	m, _ := accountTestModel()
	m.account.screen = passwordEntry
	m.account.secret.field = 2
	m.account.secret.values = [3][]byte{[]byte("old"), []byte("new-password-123"), []byte("different")}
	m, c := keyModel(m, code(tea.KeyEnter))
	if c != nil || m.account.secret.field != 0 || m.account.secret.values[0] != nil {
		t.Fatal("mismatch retained secrets")
	}
	m, _ = keyModel(m, tea.KeyPressMsg{Code: 'x', Text: "private"})
	n, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	m = n.(Model)
	if strings.Contains(m.View().Content, "private") {
		t.Fatal("resize exposed input")
	}
	buf := m.account.secret.values[0]
	m, c = keyModel(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if c == nil || m.account.secret.values[0] != nil {
		t.Fatal("interrupt not handled")
	}
	for _, b := range buf {
		if b != 0 {
			t.Fatal("interrupted buffer retained")
		}
	}
}
