//go:build windows

package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateHelperCommandKeepsCurrentTerminal(t *testing.T) {
	target := `C:\Users\sockt\sockt.exe`
	helper := filepath.Join(filepath.Dir(target), "sockt-updater.exe")
	staged := filepath.Join(filepath.Dir(target), ".sockt-update-new.exe")

	cmd := updateHelperCommand(helper, target, staged)
	if cmd.Dir != filepath.Dir(target) {
		t.Fatalf("helper directory = %q, want %q", cmd.Dir, filepath.Dir(target))
	}
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatal("helper must inherit the caller's standard terminal handles")
	}
	if cmd.SysProcAttr != nil {
		t.Fatal("helper must not create a separate console")
	}
}
