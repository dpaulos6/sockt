package tui

import (
	"bytes"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sockt/internal/protocol"
)

type page uint8

const (
	chatPage page = iota
	accountPage
	settingsPage
	administrationPage
)

type accountScreen uint8

const (
	overview accountScreen = iota
	devices
	passwordEntry
	codesEntry
	confirmation
	recoveryDisplay
)

type accountModel struct {
	device     string
	screen     accountScreen
	selected   int
	sessions   []protocol.SessionInfo
	secret     secretInput
	codes      []string
	codeOffset int
	viewScroll int
	busy       bool
	notice     string
	action     string
	target     protocol.SessionInfo
	signedOut  bool
}

func (a *accountModel) clearSecrets() { a.secret.clear(); clear(a.codes); a.codes = nil }

func (m Model) openAccount() (tea.Model, tea.Cmd) {
	m.page = accountPage
	m.composer.Blur()
	return m, nil
}
func (m Model) accountKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := &m.account
	key := k.String()
	// An in-flight mutation must deliver its result before navigation or exit.
	if a.busy {
		return m, nil
	}
	if a.screen == recoveryDisplay {
		switch key {
		case "up":
			a.codeOffset = max(0, a.codeOffset-1)
		case "down":
			a.codeOffset = min(max(0, len(a.codes)-1), a.codeOffset+1)
		case "a":
			a.clearSecrets()
			a.screen = overview
			a.notice = "Recovery codes acknowledged."
		}
		return m, nil
	}
	if key == "ctrl+c" {
		a.clearSecrets()
		return m, tea.Quit
	}
	if a.signedOut {
		return m, nil
	}
	if key == "pgdown" {
		a.viewScroll += max(1, m.height-3)
		return m, nil
	}
	if key == "pgup" {
		a.viewScroll = max(0, a.viewScroll-max(1, m.height-3))
		return m, nil
	}
	if key == "enter" || key == "up" || key == "down" || key == "esc" {
		a.viewScroll = 0
	}
	if key == "esc" {
		a.clearSecrets()
		a.screen = overview
		a.selected = 0
		m.page = chatPage
		if m.online {
			m.composer.Focus()
		}
		return m, nil
	}
	switch a.screen {
	case overview:
		switch key {
		case "up":
			a.selected = (a.selected + 3) % 4
		case "down":
			a.selected = (a.selected + 1) % 4
		case "enter":
			a.notice = ""
			switch a.selected {
			case 0:
				return m.startAccount("sessions")
			case 1:
				a.screen = passwordEntry
			case 2:
				a.screen = codesEntry
				a.notice = "Generating replacements invalidates ALL previous recovery codes."
			case 3:
				a.screen = confirmation
				a.action = "logout"
			}
		}
	case devices:
		switch key {
		case "up":
			a.selected = max(0, a.selected-1)
		case "down":
			a.selected = min(max(0, len(a.sessions)-1), a.selected+1)
		case "r":
			return m.startAccount("sessions")
		case "enter":
			if len(a.sessions) > 0 {
				a.target = a.sessions[a.selected]
				a.action = "revoke"
				a.screen = confirmation
			}
		}
	case confirmation:
		if key == "n" {
			a.screen = overview
			a.selected = 0
		}
		if key == "y" || key == "enter" {
			return m.startAccount(a.action)
		}
	case passwordEntry, codesEntry:
		if key == "enter" {
			if a.screen == codesEntry {
				return m.startAccount("codes")
			}
			if a.secret.field == 1 && (len(a.secret.values[1]) < 12 || len(a.secret.values[1]) > 128) {
				a.notice = "New password must be 12–128 bytes."
				return m, nil
			}
			if a.secret.field < 2 {
				a.secret.field++
				a.notice = ""
			} else if !bytes.Equal(a.secret.values[1], a.secret.values[2]) {
				a.secret.clear()
				a.notice = "Passwords did not match. Start again."
			} else {
				return m.startAccount("password")
			}
		} else {
			a.secret.key(k)
		}
	}
	return m, nil
}

func (m Model) accountView() tea.View {
	a := m.account
	width := max(1, m.width)
	height := max(1, m.height)
	rows := []string{"Account · " + cleanText(m.username), "Device: " + cleanText(a.device)}
	switch a.screen {
	case overview:
		for i, s := range []string{"Connected devices", "Change password", "Replace recovery codes", "Log out"} {
			prefix := "  "
			if i == a.selected {
				prefix = "› "
			}
			rows = append(rows, prefix+s)
		}
	case devices:
		if len(a.sessions) == 0 {
			rows = append(rows, "No active sessions.")
		}
		// One selected device at a time keeps all its metadata readable on small screens.
		if len(a.sessions) > 0 {
			s := a.sessions[a.selected]
			current := ""
			if s.Current {
				current = " [THIS DEVICE]"
			}
			rows = append(rows, fmt.Sprintf("%d/%d %s%s", a.selected+1, len(a.sessions), cleanText(s.Device), current), "Last active: "+s.LastSeenAt.Local().Format("2006-01-02 15:04"), "Expires: "+s.ExpiresAt.Local().Format("2006-01-02 15:04"))
		}
	case passwordEntry, codesEntry:
		labels := []string{"Current password", "New password (12–128 bytes)", "Confirm new password"}
		rows = []string{"Change password", labels[a.secret.field], "[hidden input]", "Paste disabled"}
		if a.screen == codesEntry {
			rows = []string{"Recovery codes", "Old codes invalid!", "Current password", "[hidden input]"}
		}
	case confirmation:
		prompt := "Log out this device?"
		if a.action == "revoke" {
			prompt = "Revoke " + cleanText(a.target.Device) + "?"
			if a.target.Current {
				prompt += " This signs you out."
			}
		}
		rows = append(rows, prompt, "Enter/Y confirm · N cancel")
	case recoveryDisplay:
		rows = []string{"Recovery codes · save ALL codes"}
		for i := a.codeOffset; i < len(a.codes); i++ {
			rows = append(rows, fmt.Sprintf("%d. %s", i+1, cleanText(a.codes[i])))
		}
	}
	hint := "↑↓ Enter · Esc chat"
	if a.screen == devices {
		hint = "↑↓ device · PgDn details · Enter revoke · R refresh · Esc chat"
	}
	if a.screen == recoveryDisplay {
		hint = "A saved · ↑↓ codes"
	}
	if a.busy {
		hint = "Working… please wait"
	}
	// Wrap plain text first, style afterward, and leave a stable status/hint area.
	body := []string{}
	focusEnd := 0
	for i, row := range rows {
		body = append(body, wrapText(row, width)...)
		if a.screen == overview && i == a.selected+2 {
			focusEnd = len(body)
		}
	}
	available := max(1, height-3)
	start := min(a.viewScroll, max(0, len(body)-available))
	// Keep menu selection and credential prompt visible on short terminals.
	if a.screen == overview && a.viewScroll == 0 {
		start = max(0, focusEnd-available)
	}
	body = body[start:]
	if len(body) > available {
		body = body[:available]
	}
	for len(body) < available {
		body = append(body, "")
	}
	notice := a.notice
	if notice == "" {
		notice = m.status
	}
	update := ""
	if m.updateOffer != nil {
		update = " · update available in Chat"
	}
	content := textStyle.Render(strings.Join(body, "\n")) + "\n" + labelStyle.Render(trimCells(cleanText(notice), width)) + "\n" + labelStyle.Render(trimCells(hint, width)) + "\n" + labelStyle.Render(trimCells(cleanText(m.status+update), width))
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Sockt · Account"
	return v
}

func (m Model) startAccount(action string) (tea.Model, tea.Cmd) {
	cmd := m.accountRequest(action)
	return m, cmd
}
