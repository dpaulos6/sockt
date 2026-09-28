package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"sockt/internal/client"
	"sockt/internal/config"
	"sockt/internal/protocol"
	"sockt/internal/tui"
	"sockt/internal/updater"
)

func prompt(reader *bufio.Reader, label, initial string) string {
	if initial != "" {
		fmt.Printf("%s [%s]: ", label, initial)
	} else {
		fmt.Print(label + ": ")
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(line)
	if result == "" {
		return initial
	}
	return result
}
func secretPrompt(label string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("%s requires an interactive terminal to avoid echoing credentials", label)
	}
	fmt.Print(label + ": ")
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Onboarding runs outside the alternate-screen chat UI so secrets never echo.
// forceLogin is used by `sockt login`; recovery is a separate command.
func onboard(store *config.Store, forceLogin bool, forceRecovery bool) error {
	reader := bufio.NewReader(os.Stdin)
	old := store.Value
	fmt.Print("\nSockt · account setup\n\n")
	serverDefault := old.Server
	if serverDefault == "" || serverDefault == config.LocalURL {
		serverDefault = config.DefaultRemoteURL
	}
	addr := prompt(reader, "Server WebSocket URL", serverDefault)
	if err := config.ValidateServerAddress(addr); err != nil {
		return err
	}
	username := prompt(reader, "Username", old.Username)
	if !protocol.ValidUsername(username) {
		return fmt.Errorf("username must use 1-24 letters, digits or underscores")
	}
	mode := "l"
	if forceRecovery {
		mode = "f"
	} else if !forceLogin {
		mode = strings.ToLower(prompt(reader, "[R]egister, [L]og in or [F]orgot password?", "l"))
	}
	device, err := askDevice(reader, old.DeviceLabel)
	if err != nil {
		return err
	}
	var token string
	var codes []string
	switch mode {
	case "r", "register":
		invite, err := secretPrompt("One-time invitation code")
		if err != nil {
			return err
		}
		pass, err := askNewPassword()
		if err != nil {
			return err
		}
		token, codes, err = client.RegisterWithRecovery(addr, username, strings.TrimSpace(invite), pass, device)
		if err != nil {
			return err
		}
	case "l", "login":
		pass, err := secretPrompt("Password")
		if err != nil {
			return err
		}
		token, err = client.PasswordLoginDevice(addr, username, pass, device)
		if err != nil {
			return err
		}
	case "f", "recover", "forgot":
		code, err := secretPrompt("Unused recovery code")
		if err != nil {
			return err
		}
		pass, err := askNewPassword()
		if err != nil {
			return err
		}
		token, codes, err = client.RecoverPassword(addr, username, code, pass, device)
		if err != nil {
			return err
		}
		fmt.Println("Password reset. All previous device sessions were revoked.")
	default:
		return fmt.Errorf("select register (r), login (l), or recover (f)")
	}
	style := old.Style
	if style == "" {
		style = "minimal"
	}
	if !forceLogin && !forceRecovery && mode != "l" {
		style = prompt(reader, "Appearance [minimal/classic]", style)
		if style != "classic" {
			style = "minimal"
		}
	}
	// Persist the session before displaying codes: otherwise a terminal
	// disconnect after successful registration could strand the account.
	cfg := config.Config{Version: 3, Server: addr, Username: username, Token: token, Style: style, Quiet: old.Quiet, LastSeen: 0, DeviceLabel: device}
	if err := store.Save(cfg); err != nil {
		return fmt.Errorf("login succeeded, but could not save local session: %w", err)
	}
	if mode == "r" || mode == "register" || mode == "f" || mode == "recover" || mode == "forgot" {
		showRecoveryCodes(codes)
		if len(codes) > 0 {
			fmt.Print("Press Enter after storing your codes securely...")
			_, _ = reader.ReadString('\n')
		}
	}
	fmt.Println("You're signed in. Run `sockt` to chat.")
	return nil
}
func usage() {
	fmt.Println(`Sockt v0.9 — terminal messenger

  sockt               Open chat
  sockt setup         Register, log in, or recover access
  sockt login         Log in with username and password
  sockt recover       Recover your account with a one-use code
  sockt account       Manage password, devices, and recovery codes
  sockt logout        Revoke this device's session
  /account or Ctrl+P  Open account management from the chat UI
  F2                  Install a verified update if available
  sockt version       Print version

Remote servers require wss:// and a valid trusted TLS certificate.`)
}
func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			usage()
			return
		case "version":
			fmt.Println("sockt v" + updater.CurrentVersion)
			return
		}
	}
	path, err := config.Path()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	store, err := config.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(args) > 0 {
		switch args[0] {
		case "setup", "login", "recover":
			err = onboard(store, args[0] == "login", args[0] == "recover")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Account:", err)
				os.Exit(1)
			}
			return
		case "account":
			_, err = runAccountMenu(store)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Account:", err)
				os.Exit(1)
			}
			return
		case "logout":
			if store.Value.Token == "" {
				fmt.Println("Already logged out")
				return
			}
			c := store.Value
			if err = client.Logout(c.Server, c.Username, c.Token); err != nil {
				fmt.Fprintln(os.Stderr, "Logout:", err)
				os.Exit(1)
			}
			c.Token = ""
			c.LastSeen = 0
			if err = store.Save(c); err != nil {
				fmt.Fprintln(os.Stderr, "Save:", err)
				os.Exit(1)
			}
			fmt.Println("Logged out. Run sockt login to return.")
			return
		}
	}
	if store.Value.Token == "" || store.Value.Server == "" {
		fmt.Println("Sockt needs an account before chatting.")
		if err = onboard(store, false, false); err != nil {
			fmt.Fprintln(os.Stderr, "Account:", err)
			os.Exit(1)
		}
	}
	flags := flag.NewFlagSet("sockt", flag.ExitOnError)
	defaultServer := store.Value.Server
	if env := os.Getenv("SOCKT_URL"); env != "" {
		defaultServer = env
	}
	addr := flags.String("server", defaultServer, "WebSocket server URL")
	style := flags.String("style", store.Value.Style, "minimal or classic")
	quiet := flags.Bool("quiet", store.Value.Quiet, "Suppress nonessential notices")
	_ = flags.Parse(args)
	if *style != "minimal" && *style != "classic" {
		fmt.Fprintln(os.Stderr, "Invalid style")
		os.Exit(2)
	}
	if err = config.ValidateServerAddress(*addr); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid server:", err)
		os.Exit(2)
	}
	if *addr != store.Value.Server {
		fmt.Fprintln(os.Stderr, "Cannot reuse a session with a different server; run sockt setup")
		os.Exit(2)
	}
	for {
		c := store.Value
		if c.Token == "" {
			return
		}
		session := client.New(*addr, c.Username, c.Token, c.LastSeen, store.UpdateSeen)
		program := tea.NewProgram(tui.New(c.Username, session, *style, *quiet, *addr))
		final, runErr := program.Run()
		session.Close()
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "Sockt UI:", runErr)
			os.Exit(1)
		}
		model, ok := final.(tui.Model)
		if !ok {
			return
		}
		if model.UpgradePath() != "" {
			fmt.Println("Installing verified Sockt update and restarting...")
			if err = updater.InstallAndRestart(model.UpgradePath()); err != nil {
				fmt.Fprintln(os.Stderr, "Sockt update:", err)
				os.Exit(1)
			}
			return
		}
		if !model.AccountRequested() {
			return
		}
		again, accountErr := runAccountMenu(store)
		if accountErr != nil {
			fmt.Fprintln(os.Stderr, "Account:", accountErr)
			return
		}
		if !again {
			return
		}
	}
}
