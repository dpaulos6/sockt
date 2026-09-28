//go:build !windows

package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// InstallAndRestart replaces the executable and execs the new binary in the
// same foreground terminal; the user retains their existing shell session.
func InstallAndRestart(staged string) error {
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	backup := target + ".previous"
	if err = os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(target, backup); err != nil {
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err = os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		return fmt.Errorf("install downloaded executable: %w", err)
	}
	if err = syscall.Exec(target, []string{target}, os.Environ()); err != nil {
		_ = os.Rename(target, staged)
		_ = os.Rename(backup, target)
		return fmt.Errorf("restart updated Sockt: %w", err)
	}
	return nil
}
