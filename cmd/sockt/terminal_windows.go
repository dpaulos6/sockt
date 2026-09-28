//go:build windows

package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

// newTerminalProgram uses fresh read/write console handles. This also lets a
// v0.9.2 client recover from the v0.9.1 helper, which passed CONIN$ read-only
// and therefore prevented Bubble Tea v2 from enabling raw input mode.
func newTerminalProgram(model tea.Model) (*tea.Program, func(), error) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open console input: %w", err)
	}
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		_ = input.Close()
		return nil, nil, fmt.Errorf("open console output: %w", err)
	}
	return tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output)), func() {
		_ = input.Close()
		_ = output.Close()
	}, nil
}
