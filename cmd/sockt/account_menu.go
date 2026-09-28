package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"sockt/internal/client"
	"sockt/internal/config"
	"sockt/internal/protocol"
)

func defaultDevice() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows PC"
	case "darwin":
		return "Mac"
	case "linux":
		return "Linux device"
	default:
		return "Sockt device"
	}
}

func askDevice(reader *bufio.Reader, current string) (string, error) {
	if current == "" {
		current = defaultDevice()
	}
	label := prompt(reader, "Device name", current)
	if !protocol.ValidDeviceLabel(label) {
		return "", fmt.Errorf("device name must be 1-64 printable characters")
	}
	return label, nil
}

func askNewPassword() (string, error) {
	pass, err := secretPrompt("New password (12-128 bytes)")
	if err != nil {
		return "", err
	}
	if len(pass) < 12 || len(pass) > 128 {
		return "", fmt.Errorf("password must be 12-128 bytes")
	}
	confirm, err := secretPrompt("Confirm new password")
	if err != nil {
		return "", err
	}
	if pass != confirm {
		return "", fmt.Errorf("passwords don't match")
	}
	return pass, nil
}

func showRecoveryCodes(codes []string) {
	if len(codes) == 0 {
		fmt.Println("The connected server did not provide recovery codes. Upgrade socktd before distributing v0.9.")
		return
	}
	fmt.Println("\nIMPORTANT: Save these recovery codes securely. Each works once.")
	fmt.Println("They will NOT be shown again. Never share them or save them in Git.")
	for i, code := range codes {
		fmt.Printf("  %d. %s\n", i+1, code)
	}
	fmt.Println("If you lose all codes AND your password, there is no self-service recovery.")
}

// runAccountMenu operates outside the alternate-screen chat UI. Secret prompts
// use the terminal's no-echo password reader and never enter TUI history.
func runAccountMenu(store *config.Store) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		account := store.Value
		if account.Token == "" {
			return false, fmt.Errorf("no session; run sockt login")
		}
		fmt.Printf("\nSockt · %s · Account management\n", account.Username)
		fmt.Println("1) Connected devices and revoke session")
		fmt.Println("2) Change password (logs out other devices)")
		fmt.Println("3) Generate new recovery codes (replaces old codes)")
		fmt.Println("4) Log out this device")
		fmt.Println("5) Return to chat")
		switch strings.TrimSpace(prompt(reader, "Choose [1-5]", "5")) {
		case "1":
			sessions, err := client.ListSessions(account.Server, account.Username, account.Token)
			if err != nil {
				fmt.Println("Could not fetch sessions:", err)
				continue
			}
			if len(sessions) == 0 {
				fmt.Println("No active sessions returned.")
				continue
			}
			for i, s := range sessions {
				current := ""
				if s.Current {
					current = " [THIS DEVICE]"
				}
				fmt.Printf("%d) %s%s · Last active %s · Expires %s\n", i+1, s.Device, current,
					s.LastSeenAt.Local().Format("2006-01-02 15:04"), s.ExpiresAt.Local().Format("2006-01-02"))
			}
			selection := strings.TrimSpace(prompt(reader, "Session number to revoke (Enter to cancel)", ""))
			if selection == "" {
				continue
			}
			index, err := strconv.Atoi(selection)
			if err != nil || index < 1 || index > len(sessions) {
				fmt.Println("Invalid number")
				continue
			}
			choice := sessions[index-1]
			if strings.ToLower(prompt(reader, "Revoke this session? [y/N]", "n")) != "y" {
				continue
			}
			if err = client.RevokeSession(account.Server, account.Username, account.Token, choice.ID); err != nil {
				fmt.Println("Could not revoke:", err)
				continue
			}
			fmt.Println("Session revoked. An already connected device may disconnect on its next heartbeat.")
			if choice.Current {
				account.Token = ""
				account.LastSeen = 0
				if err = store.Save(account); err != nil {
					return false, err
				}
				return false, nil
			}
		case "2":
			oldPassword, err := secretPrompt("Current password")
			if err != nil {
				return false, err
			}
			newPassword, err := askNewPassword()
			if err != nil {
				fmt.Println(err)
				continue
			}
			newToken, err := client.ChangePassword(account.Server, account.Username, account.Token, oldPassword, newPassword, account.DeviceLabel)
			if err != nil {
				fmt.Println("Password change failed:", err)
				continue
			}
			account.Token = newToken
			if err = store.Save(account); err != nil {
				return false, fmt.Errorf("password changed, but could not save new session: %w; run sockt login", err)
			}
			fmt.Println("Password changed. Your other sessions were revoked.")
		case "3":
			password, err := secretPrompt("Confirm account password")
			if err != nil {
				return false, err
			}
			codes, err := client.GenerateRecoveryCodes(account.Server, account.Username, account.Token, password)
			if err != nil {
				fmt.Println("Could not generate recovery codes:", err)
				continue
			}
			showRecoveryCodes(codes)
			fmt.Print("Press Enter after storing them securely...")
			_, _ = reader.ReadString('\n')
		case "4":
			if strings.ToLower(prompt(reader, "Log out this device? [y/N]", "n")) != "y" {
				continue
			}
			if err := client.Logout(account.Server, account.Username, account.Token); err != nil {
				return false, err
			}
			account.Token = ""
			account.LastSeen = 0
			if err := store.Save(account); err != nil {
				return false, err
			}
			fmt.Println("Logged out. Use sockt login to return.")
			return false, nil
		case "5":
			return true, nil
		default:
			fmt.Println("Choose an option from 1 to 5")
		}
	}
}
