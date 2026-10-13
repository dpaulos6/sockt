package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"sockt/internal/protocol"
)

type adminModel struct {
	status  *protocol.AdminStatus
	input   textinput.Model
	action  string
	confirm bool
	busy    bool
	invite  string
	notice  string
	scroll  int
}

func (m *Model) clearAdmin() {
	m.adminAllowed = false
	m.admin = adminModel{}
	m.slashSelection = 0
	if m.page == administrationPage {
		m.page = chatPage
		if m.online {
			m.composer.Focus()
		}
	}
}

func (m Model) openAdmin() (tea.Model, tea.Cmd) {
	if !m.adminAllowed {
		m.status = "Admin access is unavailable for this account"
		return m, nil
	}
	if !m.online {
		m.status = "Connect before opening Admin"
		return m, nil
	}
	m.page = administrationPage
	m.composer.Blur()
	m.admin = adminModel{input: textinput.New()}
	m.admin.input.CharLimit = 24
	m.admin.input.SetWidth(max(10, m.width-4))
	m.adminSend("admin_status", false)
	return m, nil
}

func (m *Model) adminSend(action string, confirmed bool) {
	if !m.adminAllowed || !m.online || m.connection == nil {
		m.admin.notice = "Admin service unavailable"
		return
	}
	if err := m.connection.SendAdmin(action, m.admin.input.Value(), confirmed); err != nil {
		m.admin.notice = "Request could not be sent; check your connection"
		return
	}
	m.admin.busy = true
	m.admin.notice = "Working…"
}

func (m *Model) adminComplete(p protocol.Packet) {
	if !m.adminAllowed || m.page != administrationPage {
		return
	}
	a := &m.admin
	a.busy = false
	a.confirm = false
	a.action = ""
	a.input.Blur()
	a.notice = p.Text
	if p.AdminStatus != nil {
		a.status = p.AdminStatus
	}
	a.invite = p.Invite
	if p.Type == "admin_error" && p.Text == "Admin access denied" {
		m.clearAdmin()
		m.status = "Admin access denied"
	}
}

func (m Model) adminKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := &m.admin
	key := k.String()
	if key == "ctrl+c" {
		m.admin = adminModel{}
		return m, tea.Quit
	}
	if a.busy {
		return m, nil
	}
	if key == "esc" {
		if a.action != "" {
			a.action = ""
			a.confirm = false
			a.input.Blur()
			return m, nil
		}
		// Leaving the panel discards the displayed invite.
		m.admin = adminModel{}
		m.page = chatPage
		if m.online {
			m.composer.Focus()
		}
		return m, nil
	}
	if key == "pgdown" {
		a.scroll += max(1, m.height-3)
		return m, nil
	}
	if key == "pgup" {
		a.scroll = max(0, a.scroll-max(1, m.height-3))
		return m, nil
	}
	if a.confirm {
		switch strings.ToLower(key) {
		case "y", "enter":
			m.adminSend(a.action, true)
		case "n":
			a.confirm = false
			a.input.Focus()
		}
		return m, nil
	}
	if a.action != "" {
		if key == "enter" {
			if !protocol.ValidUsername(a.input.Value()) {
				a.notice = "Use 1–24 letters, digits or underscores"
				return m, nil
			}
			a.confirm = true
			a.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(k)
		return m, cmd
	}
	if a.invite != "" && strings.ToLower(key) != "a" {
		return m, nil
	}
	switch strings.ToLower(key) {
	case "r":
		m.adminSend("admin_status", false)
	case "c", "v":
		a.invite = ""
		a.notice = ""
		a.scroll = 0
		a.action = "admin_invite_create"
		if key == "v" || key == "V" {
			a.action = "admin_invite_revoke"
		}
		a.input.SetValue("")
		a.input.Focus()
	case "a":
		a.invite = ""
		a.notice = "Invite acknowledged"
	}
	return m, nil
}

func (m Model) adminView() tea.View {
	a := m.admin
	width, height := max(20, m.width), max(8, m.height)
	rows := []string{"Admin · server", ""}
	if s := a.status; s != nil {
		rows = append(rows, "Version: "+cleanText(s.Version)+" ("+cleanText(s.Commit)+")", "Uptime: "+(time.Duration(s.UptimeSeconds)*time.Second).String(), fmt.Sprintf("Connections: %d · Accounts: %d", s.Connections, s.Accounts))
	}
	rows = append(rows, "", "C create invite · V revoke unused invites · R refresh")
	if a.action != "" {
		prompt := "Create invite for username:"
		if a.action == "admin_invite_revoke" {
			prompt = "Revoke ALL unused invites for username:"
		}
		rows = append(rows, prompt, cleanText(a.input.Value()))
		if a.confirm {
			rows = append(rows, "Confirm for "+cleanText(a.input.Value())+"? Enter/Y confirm · N cancel")
		}
	}
	if a.invite != "" {
		rows = append(rows, "Invite (shown once; A acknowledge):", cleanText(a.invite))
	}
	body := []string{}
	for _, row := range rows {
		body = append(body, wrapText(row, width)...)
	}
	available := height - 3
	start := min(a.scroll, max(0, len(body)-available))
	if a.scroll == 0 && (a.action != "" || a.invite != "") {
		start = max(0, len(body)-available)
	}
	body = body[start:min(len(body), start+available)]
	for len(body) < available {
		body = append(body, "")
	}
	content := textStyle.Render(strings.Join(body, "\n")) + "\n" + labelStyle.Render(trimCells(cleanText(a.notice), width)) + "\n" + labelStyle.Render(trimCells("Esc chat · PgUp/PgDn details · Ctrl+C quit", width)) + "\n" + labelStyle.Render(trimCells(cleanText(m.status), width))
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Sockt · Admin"
	return v
}
