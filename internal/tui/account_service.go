package tui

import (
	tea "charm.land/bubbletea/v2"

	"sockt/internal/client"
	"sockt/internal/config"
	"sockt/internal/protocol"
)

// AccountService keeps network and persistence work outside visual components.
// Errors are deliberately mapped to fixed UI messages, never echoed verbatim.
type AccountService interface {
	Sessions() ([]protocol.SessionInfo, error)
	Revoke(string) error
	Password(string, string) (string, error)
	Codes(string) ([]string, error)
	Logout() error
	SaveToken(string) error
	Connect() *client.Client
}

type localAccount struct{ store *config.Store }

func (s localAccount) Sessions() ([]protocol.SessionInfo, error) {
	c := s.store.Snapshot()
	return client.ListSessions(c.Server, c.Username, c.Token)
}
func (s localAccount) Revoke(id string) error {
	c := s.store.Snapshot()
	return client.RevokeSession(c.Server, c.Username, c.Token, id)
}
func (s localAccount) Password(old, next string) (string, error) {
	c := s.store.Snapshot()
	return client.ChangePassword(c.Server, c.Username, c.Token, old, next, c.DeviceLabel)
}
func (s localAccount) Codes(password string) ([]string, error) {
	c := s.store.Snapshot()
	return client.GenerateRecoveryCodes(c.Server, c.Username, c.Token, password)
}
func (s localAccount) Logout() error {
	c := s.store.Snapshot()
	return client.Logout(c.Server, c.Username, c.Token)
}
func (s localAccount) SaveToken(token string) error { return s.store.SetToken(token) }
func (s localAccount) Connect() *client.Client {
	c := s.store.Snapshot()
	return client.New(c.Server, c.Username, c.Token, c.LastSeen, s.store.UpdateSeen)
}
func (m Model) WithAccount(store *config.Store) Model {
	m.accountService = localAccount{store}
	m.account.device = store.Snapshot().DeviceLabel
	return m
}

// Close releases whichever chat transport is current after token rotation.
func (m Model) Close() {
	if m.connection != nil {
		m.connection.Close()
	}
	m.account.clearSecrets()
}

type accountResult struct {
	action                           string
	sessions                         []protocol.SessionInfo
	codes                            []string
	failed, storageFailed, signedOut bool
}

func (m *Model) accountRequest(action string) tea.Cmd {
	a := &m.account
	if a.busy || m.accountService == nil {
		a.notice = "Account service unavailable"
		return nil
	}
	a.busy = true
	a.notice = "Working…"
	service, conn, target := m.accountService, m.connection, a.target
	// Transfer ownership of buffers to the command, then remove them from the model.
	secrets := a.secret.values
	a.secret = secretInput{}
	return func() tea.Msg {
		defer func() {
			for _, b := range secrets {
				clear(b)
			}
		}()
		r := accountResult{action: action}
		var err error
		switch action {
		case "sessions":
			r.sessions, err = service.Sessions()
		case "revoke":
			err = service.Revoke(target.ID)
			r.signedOut = err == nil && target.Current
		case "logout":
			err = service.Logout()
			r.signedOut = err == nil
		case "codes":
			r.codes, err = service.Codes(string(secrets[0]))
		case "password":
			var token string
			token, err = service.Password(string(secrets[0]), string(secrets[1]))
			if err == nil {
				if conn != nil {
					conn.CloseAndWait()
				}
				if service.SaveToken(token) != nil {
					r.storageFailed = true
					r.signedOut = true
				}
			}
		}
		if r.signedOut && !r.storageFailed {
			if conn != nil {
				conn.CloseAndWait()
			}
			r.storageFailed = service.SaveToken("") != nil
		}
		r.failed = err != nil
		return r
	}
}

func (m Model) accountComplete(r accountResult) (tea.Model, tea.Cmd) {
	a := &m.account
	a.busy = false
	a.viewScroll = 0
	a.clearSecrets()
	if r.failed {
		a.screen = overview
		a.selected = 0
		a.notice = "Request failed. Check your connection and credentials, then retry."
		return m, nil
	}
	if r.signedOut {
		m.events = nil
		m.status = "Signed out"
		a.signedOut = true
		a.screen = overview
		m.online = false
		m.composer.Blur()
		a.notice = "Signed out. Ctrl+C to exit; run sockt login to return."
		if r.storageFailed {
			a.notice = "Session changed, but local save failed. Exit and run sockt login before returning."
		}
		return m, nil
	}
	switch r.action {
	case "sessions":
		a.sessions = r.sessions
		a.selected = 0
		a.screen = devices
		a.notice = "Select a device to revoke."
	case "revoke":
		a.screen = overview
		a.selected = 0
		a.notice = "Session revoked. The device disconnects on its next heartbeat."
	case "password":
		a.screen = overview
		a.notice = "Password changed. Other devices signed out."
		m.connection = m.accountService.Connect()
		m.events = m.connection.Incoming()
		m.online = false
		return m, waitForNetwork(m.events)
	case "codes":
		a.screen = recoveryDisplay
		a.codes = r.codes
		a.codeOffset = 0
		a.notice = "Old codes are invalid. Save every new code securely."
	}
	return m, nil
}
