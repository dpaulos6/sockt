//go:build !windows

package main

import tea "charm.land/bubbletea/v2"

func newTerminalProgram(model tea.Model) (*tea.Program, func(), error) {
	return tea.NewProgram(model), func() {}, nil
}
