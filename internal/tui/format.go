package tui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"sockt/internal/protocol"
)

func nameStyle(username string) lipgloss.Style {
	const colors = 6
	palette := [colors]string{
		"#76D7AC", "#A8C6FF", "#E0B1FF", "#F3C887", "#FFB4A7", "#9FDAD7",
	}
	var hash uint32
	for _, r := range username {
		hash = hash*33 + uint32(r)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette[hash%colors]))
}

// Clean server-supplied text before it enters the renderer. In particular,
// embedded escape bytes must never be interpreted as terminal control codes.
func cleanText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

func trimCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var out strings.Builder
	for _, r := range s {
		if lipgloss.Width(out.String()+string(r)+"…") > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

// wrapText returns visual rows that fit in the available terminal-cell width.
// This prototype normalizes repeated whitespace when wrapping chat messages.
func wrapText(text string, width int) []string {
	width = max(1, width)
	words := strings.Fields(cleanText(text))
	if len(words) == 0 {
		return []string{""}
	}

	var result []string
	line := ""
	appendLine := func() {
		if line != "" {
			result = append(result, line)
			line = ""
		}
	}

	for _, word := range words {
		// Handle words longer than the content column without overflowing it.
		for lipgloss.Width(word) > width {
			appendLine()
			cut := 0
			used := 0
			for i, r := range word {
				cells := lipgloss.Width(string(r))
				if used+cells > width && cut > 0 {
					break
				}
				cut = i + len(string(r)) // byte boundary, not rune count
				used += cells
			}
			// Width=1 and a double-width rune: preserve progress.
			if cut == 0 {
				_, size := runeAtStart(word)
				cut = size
			}
			result = append(result, word[:cut])
			word = word[cut:]
		}
		if word == "" {
			continue
		}
		if line == "" {
			line = word
		} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= width {
			line += " " + word
		} else {
			appendLine()
			line = word
		}
	}
	appendLine()
	return result
}

func runeAtStart(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func formatMessage(m protocol.Message, width int) []string {
	timestamp := m.SentAt.Local().Format("02/01 15:04")
	nameWidth := 12
	if width >= 72 {
		nameWidth = 16
	} else if width < 52 {
		nameWidth = 8
	}
	if width < 40 {
		timestamp = m.SentAt.Local().Format("15:04")
	}
	shortName := trimCells(m.Username, nameWidth)
	name := nameStyle(m.Username).Render(fmt.Sprintf("%-*s", nameWidth, shortName))
	prefix := labelStyle.Render(timestamp) + "  " + name + "  "
	prefixWidth := lipgloss.Width(prefix)
	if prefixWidth+8 > width {
		// Small window: move the content to a second row rather than overflow.
		title := labelStyle.Render(timestamp) + " " + nameStyle(m.Username).Render(trimCells(m.Username, max(3, width-9)))
		lines := wrapText(m.Text, max(1, width-2))
		rows := []string{title}
		for _, line := range lines {
			rows = append(rows, "  "+textStyle.Render(line))
		}
		return rows
	}
	parts := wrapText(m.Text, max(1, width-prefixWidth-1))
	rows := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			rows = append(rows, prefix+textStyle.Render(part))
		}
		if i > 0 {
			rows = append(rows, strings.Repeat(" ", prefixWidth)+textStyle.Render(part))
		}
	}
	return rows
}

// Minimal mode uses compact single-line entries with time, name and message.
// Long messages wrap cleanly under the text rather than clutter the terminal.
func formatCompact(m protocol.Message, width int) []string {
	stamp := m.SentAt.Local().Format("15:04")
	nameLimit := 12
	if width < 50 {
		nameLimit = 9
	}
	if width < 33 {
		nameLimit = 6
	}
	name := trimCells(m.Username, nameLimit)
	prefix := labelStyle.Render(stamp) + "  " + nameStyle(m.Username).Render(name) + "  "
	prefixW := lipgloss.Width(prefix)
	if prefixW+6 > width {
		title := labelStyle.Render(stamp) + " " + nameStyle(m.Username).Render(trimCells(name, max(3, width-8)))
		out := []string{title}
		for _, line := range wrapText(m.Text, max(1, width-2)) {
			out = append(out, "  "+textStyle.Render(line))
		}
		return out
	}
	parts := wrapText(m.Text, max(1, width-prefixW))
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			out = append(out, prefix+textStyle.Render(part))
		} else {
			out = append(out, strings.Repeat(" ", prefixW)+textStyle.Render(part))
		}
	}
	return out
}
