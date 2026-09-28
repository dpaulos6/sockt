// Package tui controls terminal rendering. Network goroutines NEVER print to
// stdout: they send events to the Bubble Tea update loop instead.
package tui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sockt/internal/updater"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sockt/internal/client"
	"sockt/internal/protocol"
)

const maxLocalMessages = 1200

var (
	brandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#86CBB0"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#929CAA"))
	lineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#444B55"))
	textStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E5E9"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F28B82"))
)

type Model struct {
	username              string
	connection            *client.Client
	events                <-chan client.Event
	composer              textinput.Model
	history               []protocol.Message
	status                string
	width, height, scroll int
	online                bool
	members               int
	style                 string
	quiet                 bool
	help                  bool
	accountRequested      bool
	updateServer          string
	updateOffer           *updater.Offer
	updateStage           string
	updateStagedPath      string
}

func New(username string, conn *client.Client, style string, quiet bool, serverURL string) Model {
	composer := textinput.New()
	composer.Prompt = "> "
	composer.Placeholder = "Write a message…"
	composer.CharLimit = protocol.MaxMessageBytes
	composer.SetVirtualCursor(false)
	composer.SetWidth(74)
	composer.Focus()
	return Model{username: username, connection: conn, events: conn.Incoming(), composer: composer, status: "Connecting…", width: 80, height: 24, style: style, quiet: quiet, updateServer: serverURL}
}

type closedMsg struct{}

func waitForNetwork(events <-chan client.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			return closedMsg{}
		}
		return e
	}
}

type updateCheckMsg struct {
	offer *updater.Offer
	err   error
}
type updateTickMsg struct{}
type updateDownloadMsg struct {
	path string
	err  error
}

func checkForUpdate(server string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		offer, err := updater.Check(ctx, server, updater.CurrentVersion, updater.PublicKeyHex, nil)
		return updateCheckMsg{offer: offer, err: err}
	}
}
func updateTick() tea.Cmd {
	return tea.Tick(30*time.Minute, func(time.Time) tea.Msg { return updateTickMsg{} })
}
func downloadUpdate(offer updater.Offer) tea.Cmd {
	return func() tea.Msg {
		path, err := os.Executable()
		if err != nil {
			return updateDownloadMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		staged, err := updater.Download(ctx, offer, path, &http.Client{Timeout: 3 * time.Minute})
		return updateDownloadMsg{path: staged, err: err}
	}
}

// UpgradePath is nonempty only after a verified update was staged and the TUI
// voluntarily exited. The CLI installs and restarts outside alternate screen.
func (m Model) UpgradePath() string    { return m.updateStagedPath }
func (m Model) AccountRequested() bool { return m.accountRequested }

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForNetwork(m.events)}
	if updater.PublicKeyHex != "" {
		cmds = append(cmds, checkForUpdate(m.updateServer), updateTick())
	}
	return tea.Batch(cmds...)
}
func (m *Model) addMessage(msg protocol.Message) {
	if msg.ID <= 0 {
		return
	}
	at := sort.Search(len(m.history), func(i int) bool { return m.history[i].ID >= msg.ID })
	if at < len(m.history) && m.history[at].ID == msg.ID {
		return
	}
	before := len(m.chatRows(m.width))
	m.history = append(m.history, protocol.Message{})
	copy(m.history[at+1:], m.history[at:])
	m.history[at] = msg
	if len(m.history) > maxLocalMessages {
		m.history = m.history[len(m.history)-maxLocalMessages:]
	}
	if m.scroll > 0 {
		m.scroll += max(0, len(m.chatRows(m.width))-before)
	}
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateTickMsg:
		if m.updateStage == "" {
			return m, tea.Batch(checkForUpdate(m.updateServer), updateTick())
		}
		return m, updateTick()
	case updateCheckMsg:
		// Update availability never blocks login or normal messaging.
		if msg.err == nil && m.updateStage == "" {
			m.updateOffer = msg.offer
		}
		return m, nil
	case updateDownloadMsg:
		if msg.err != nil {
			m.updateStage = ""
			m.status = "Update failed: " + msg.err.Error()
			return m, nil
		}
		m.updateStagedPath = msg.path
		m.updateStage = "ready"
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = max(20, msg.Width)
		m.height = max(8, msg.Height)
		m.composer.SetWidth(max(10, m.width-4))
		return m, nil
	case closedMsg:
		m.online = false
		m.composer.Blur()
		if m.status == "Connected" {
			m.status = "Disconnected · run sockt setup if your invite changed"
		}
		return m, nil
	case client.Event:
		if msg.Reset {
			m.history = nil
			m.scroll = 0
		}
		if msg.Status != "" {
			m.online = msg.Online
			m.status = msg.Status
			if msg.Online {
				m.composer.Focus()
			} else {
				m.composer.Blur()
			}
		}
		switch msg.Packet.Type {
		case "history":
			for _, item := range msg.Packet.Messages {
				m.addMessage(item)
			}
		case "message":
			if msg.Packet.Message != nil {
				m.addMessage(*msg.Packet.Message)
			}
		case "presence":
			m.members = len(msg.Packet.Users)
		case "system":
			if !m.quiet {
				m.status = msg.Packet.Text
			}
		case "error":
			m.status = "Server: " + msg.Packet.Text
		}
		return m, waitForNetwork(m.events)
	case tea.KeyPressMsg:
		if m.updateStage == "confirm" {
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				m.updateStage = "downloading"
				m.status = "Downloading and verifying Sockt " + m.updateOffer.Version + "…"
				return m, downloadUpdate(*m.updateOffer)
			case "n", "esc":
				m.updateStage = ""
				return m, nil
			}
			return m, nil
		}
		if m.updateStage == "downloading" {
			// Do not take input that suggests a chat send during an update.
			return m, nil
		}
		switch msg.String() {
		case "f2":
			if m.updateOffer != nil {
				m.updateStage = "confirm"
			}
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+p":
			m.accountRequested = true
			return m, tea.Quit
		case "f1":
			m.help = !m.help
			return m, nil
		case "pgup":
			rows := m.chatRows(m.width)
			m.scroll = min(max(0, len(rows)-m.bodyHeight()), m.scroll+max(3, m.bodyHeight()/2))
			return m, nil
		case "pgdown":
			m.scroll = max(0, m.scroll-max(3, m.bodyHeight()/2))
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.composer.Value())
			if text == "" {
				return m, nil
			}
			if text == "/exit" {
				return m, tea.Quit
			}
			if text == "/account" {
				m.accountRequested = true
				return m, tea.Quit
			}
			if text == "/help" {
				m.help = true
				m.composer.SetValue("")
				return m, nil
			}
			if !m.online {
				m.status = "Offline · message not sent; it remains in your input"
				return m, nil
			}
			if len([]byte(text)) > protocol.MaxMessageBytes {
				m.status = fmt.Sprintf("Message exceeds %d bytes", protocol.MaxMessageBytes)
				return m, nil
			}
			if err := m.connection.SendMessage(text); err != nil {
				m.status = "Not sent: " + err.Error()
				return m, nil
			}
			m.composer.SetValue("")
			m.scroll = 0
			// Wait for the authoritative server echo; never echo input locally.
			return m, nil
		}
	}
	var cmd tea.Cmd
	if m.online {
		m.composer, cmd = m.composer.Update(msg)
	}
	return m, cmd
}
func (m Model) bodyHeight() int { return max(3, m.height-5) }
func (m Model) header() string {
	channel := "general"
	if m.style == "classic" {
		channel = "#general"
	}
	left := brandStyle.Render("sockt") + labelStyle.Render("  "+channel)
	right := labelStyle.Render(fmt.Sprintf("%d online", m.members))
	if m.updateOffer != nil {
		right = brandStyle.Render("↑ v"+m.updateOffer.Version) + labelStyle.Render(fmt.Sprintf(" · %d online", m.members))
	}
	if !m.online {
		right = errorStyle.Render("offline")
	}
	if m.width < lipgloss.Width(left)+lipgloss.Width(right)+2 {
		return trimCells(left, m.width)
	}
	return left + strings.Repeat(" ", m.width-lipgloss.Width(left)-lipgloss.Width(right)) + right
}
func (m Model) footer() string {
	switch m.updateStage {
	case "confirm":
		return brandStyle.Render(trimCells("Install v"+m.updateOffer.Version+" and restart now? Y/N", m.width))
	case "downloading":
		return brandStyle.Render(trimCells("Downloading and verifying update…", m.width))
	}
	hint := "Enter send · F1 help · Ctrl+P account · PgUp/PgDn · Ctrl+C quit"
	if m.width < 61 {
		hint = "Enter send · F1 help · Ctrl+P account · Ctrl+C quit"
	}
	if m.width < 42 {
		hint = "F1 help · Ctrl+P account · Ctrl+C quit"
	}
	// Only draw extra status when it conveys something beyond ordinary uptime.
	if m.status != "Connected" && m.status != "" {
		short := trimCells(m.status, m.width)
		return labelStyle.Render(short)
	}
	if m.updateOffer != nil {
		return brandStyle.Render(trimCells("Update v"+m.updateOffer.Version+" available · F2 install & restart", m.width))
	}
	return labelStyle.Render(trimCells(hint, m.width))
}
func (m Model) chatRows(width int) []string {
	rows := make([]string, 0, len(m.history))
	for _, item := range m.history {
		if m.style == "classic" {
			rows = append(rows, formatMessage(item, width)...)
		} else {
			rows = append(rows, formatCompact(item, width)...)
		}
	}
	return rows
}
func (m Model) View() tea.View {
	width := max(20, m.width)
	height := max(8, m.height)
	separator := lineStyle.Render(strings.Repeat("─", width))
	rows := m.chatRows(width)
	if m.help {
		rows = append(rows, "", brandStyle.Render("Keyboard shortcuts"), labelStyle.Render("Enter   send message"), labelStyle.Render("PgUp    older messages"), labelStyle.Render("PgDn    newer messages"), labelStyle.Render("F1      close help"), labelStyle.Render("F2      install available update"), labelStyle.Render("Ctrl+P  account management"), labelStyle.Render("/account account management"), labelStyle.Render("/exit   leave Sockt"))
	}
	h := max(3, height-5)
	end := max(0, len(rows)-m.scroll)
	start := max(0, end-h)
	visible := rows[start:end]
	body := make([]string, 0, h)
	for i := 0; i < h-len(visible); i++ {
		body = append(body, "")
	}
	body = append(body, visible...)
	content := m.header() + "\n" + separator + "\n" + strings.Join(body, "\n") + "\n" + separator + "\n" + m.composer.View() + "\n" + m.footer()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Sockt · general"
	if cursor := m.composer.Cursor(); cursor != nil && m.online {
		cursor.Y = height - 2
		view.Cursor = cursor
	}
	return view
}
