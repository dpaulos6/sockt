package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sockt/internal/buildinfo"
	"sockt/internal/config"
	"sockt/internal/updater"
)

func updateNow(store *config.Store) error {
	if updater.PublicKeyHex == "" {
		return errors.New("this development build has no release verification key")
	}
	if store.Value.Server == "" {
		return errors.New("no configured server; run sockt setup first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	offer, err := updater.Check(ctx, store.Value.Server, buildinfo.Version, updater.PublicKeyHex, nil)
	if err != nil {
		return err
	}
	if offer == nil {
		fmt.Println("Sockt is up to date.")
		return nil
	}
	fmt.Printf("Install verified Sockt %s? [y/N] ", offer.Version)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(answer) == 0 {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
		fmt.Println("Update cancelled.")
		return nil
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	staged, err := updater.Download(ctx, *offer, target, nil)
	if err != nil {
		return err
	}
	return updater.InstallAndRestart(staged)
}

func doctor() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(exe); statErr != nil || info.IsDir() {
		return errors.New("executable is not accessible")
	}
	if runtime.GOOS == "windows" {
		helper := filepath.Join(filepath.Dir(exe), "sockt-updater.exe")
		if _, err := os.Stat(helper); err != nil {
			return errors.New("missing Windows updater helper beside sockt.exe")
		}
	}
	probe := filepath.Join(filepath.Dir(exe), ".sockt-write-check")
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("installation directory is not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	verification := "disabled for this development build"
	if updater.PublicKeyHex != "" {
		verification = "configured"
	}
	fmt.Printf("Sockt %s (%s)\nExecutable: %s\nConfig: %s\nUpdate verification: %s\n", buildinfo.Version, buildinfo.Commit, exe, path, verification)
	return nil
}
