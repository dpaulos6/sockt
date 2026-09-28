//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Windows cannot replace the executable while it is running. A separate
// helper waits for this process to exit, then swaps and restarts it.
func InstallAndRestart(staged string) error {
	target, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(filepath.Dir(target), "sockt-updater.exe")
	if _, err = os.Stat(helper); err != nil {
		return fmt.Errorf("missing sockt-updater.exe next to sockt.exe; reinstall the v0.8 distribution: %w", err)
	}
	cmd := updateHelperCommand(helper, target, staged)
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start updater helper: %w", err)
	}
	return cmd.Process.Release()
}

// updateHelperCommand keeps the helper in the terminal that launched Sockt.
// CREATE_NEW_CONSOLE would move it out of a Windows Terminal/PowerShell
// session, and Go otherwise gives a child nil standard streams NUL handles.
func updateHelperCommand(helper, target, staged string) *exec.Cmd {
	cmd := exec.Command(helper, "--target", target, "--staged", staged)
	cmd.Dir = filepath.Dir(target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
