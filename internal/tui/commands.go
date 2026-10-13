package tui

import "strings"

// Slash commands are intentionally local. Unknown slash-prefixed input is never
// forwarded as a chat message, even while disconnected.
type slashCommand struct {
	Name        string
	Description string
}

var slashCommands = []slashCommand{
	{Name: "/account", Description: "Open Account · Esc returns to chat"},
	{Name: "/help", Description: "Show keyboard shortcuts"},
	{Name: "/update", Description: "Check for or install an update"},
	{Name: "/exit", Description: "Leave Sockt"},
}

// slashCandidates returns prefix matches and whether input is an active slash
// query. Arguments aren't currently supported. The registry and execution
// switch must be kept in sync when new commands are added.
func slashCandidates(input string) ([]slashCommand, bool) {
	return slashCandidatesFor(input, false)
}

func slashCandidatesFor(input string, admin bool) ([]slashCommand, bool) {
	query := strings.TrimSpace(input)
	if !strings.HasPrefix(query, "/") || strings.ContainsAny(query, " \t\r\n") {
		return nil, false
	}
	query = strings.ToLower(query)
	matches := make([]slashCommand, 0, len(slashCommands))
	commands := slashCommands
	if admin {
		commands = append(append([]slashCommand(nil), slashCommands...), slashCommand{Name: "/admin", Description: "Open Admin · Esc returns to chat"})
	}
	for _, command := range commands {
		if strings.HasPrefix(command.Name, query) {
			matches = append(matches, command)
		}
	}
	return matches, true
}

func hasSlashPrefix(input string) bool {
	return strings.HasPrefix(strings.TrimLeft(input, " \t"), "/")
}
