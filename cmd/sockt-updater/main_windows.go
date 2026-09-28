//go:build windows

// sockt-updater is a small companion binary shipped with the v0.8 Windows
// client. It replaces the main executable after that process exits.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	target := flag.String("target", "", "absolute path of the old client")
	staged := flag.String("staged", "", "previously verified replacement")
	flag.Parse()
	if *target == "" || *staged == "" || !filepath.IsAbs(*target) || !filepath.IsAbs(*staged) || filepath.Dir(*target) != filepath.Dir(*staged) {
		fmt.Fprintln(os.Stderr, "unsafe update paths")
		os.Exit(2)
	}
	fmt.Println("Installing Sockt update; waiting for previous client to exit...")
	backup := *target + ".previous"
	for i := 0; i < 120; i++ {
		// Do not remove an existing backup until the old process can be moved.
		_ = os.Remove(backup)
		if err := os.Rename(*target, backup); err == nil {
			if err = os.Rename(*staged, *target); err != nil {
				_ = os.Rename(backup, *target)
				fmt.Fprintln(os.Stderr, "Update failed; old client restored:", err)
				time.Sleep(8 * time.Second)
				os.Exit(1)
			}
			// Go's nil stdio defaults to NUL. Explicitly attach the restarted
			// TUI to THIS helper's newly allocated Windows console.
			conIn, inErr := os.OpenFile("CONIN$", os.O_RDWR, 0)
			conOut, outErr := os.OpenFile("CONOUT$", os.O_RDWR, 0)
			if inErr != nil || outErr != nil {
				fmt.Fprintln(os.Stderr, "Installed update but could not open a console; launch Sockt manually.", inErr, outErr)
				time.Sleep(8 * time.Second)
				os.Exit(1)
			}
			cmd := exec.Command(*target)
			cmd.Dir = filepath.Dir(*target)
			cmd.Stdin = conIn
			cmd.Stdout = conOut
			cmd.Stderr = conOut
			if err = cmd.Run(); err != nil {
				fmt.Fprintln(conOut, "Sockt exited with error:", err)
				time.Sleep(8 * time.Second)
			}
			conIn.Close()
			conOut.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "Timed out waiting to replace Sockt. Close any other running clients and retry.")
	time.Sleep(8 * time.Second)
	os.Exit(1)
}
