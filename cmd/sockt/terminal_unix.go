//go:build !windows

package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"golang.org/x/term"
	"os"
)

func newTerminalProgram(model tea.Model) (*tea.Program, func(), error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, nil, fmt.Errorf("interactive terminal input and output are required")
	}
	return tea.NewProgram(model), func() {}, nil
}
