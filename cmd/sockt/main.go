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

func onboard(store *config.Store, forceLogin bool) error {
	reader := bufio.NewReader(os.Stdin)
	old := store.Value
	fmt.Print("\nSockt v0.7 · account setup\n\n")
	addr := prompt(reader, "Server WebSocket URL (wss://host/ws)", old.Server)
	if err := config.ValidateServerAddress(addr); err != nil {
		return err
	}
	username := prompt(reader, "Username", old.Username)
	if !protocol.ValidUsername(username) {
		return fmt.Errorf("username must use 1-24 letters, digits or underscores")
	}
	mode := "l"
	if !forceLogin {
		mode = strings.ToLower(prompt(reader, "Register a new account or log in? [r/l]", "r"))
	}
	var token string
	var err error
	switch mode {
	case "r", "register":
		invite, inviteErr := secretPrompt("One-time registration invite")
		if inviteErr != nil {
			return inviteErr
		}
		password, pwdErr := secretPrompt("Choose password (12+ characters)")
		if pwdErr != nil {
			return pwdErr
		}
		confirm, confirmErr := secretPrompt("Confirm password")
		if confirmErr != nil {
			return confirmErr
		}
		if password != confirm {
			return fmt.Errorf("passwords do not match")
		}
		token, err = client.Register(addr, username, strings.TrimSpace(invite), password)
		// Never save the invite code or password in a config file.
	case "l", "login":
		password, pwdErr := secretPrompt("Account password")
		if pwdErr != nil {
			return pwdErr
		}
		token, err = client.PasswordLogin(addr, username, password)
	default:
		return fmt.Errorf("choose r to register or l to log in")
	}
	if err != nil {
		return err
	}
	style := old.Style
	if style == "" {
		style = "minimal"
	}
	if !forceLogin {
		style = prompt(reader, "Appearance [minimal/classic]", "minimal")
		if style != "classic" {
			style = "minimal"
		}
	}
	cfg := config.Config{Version: 2, Server: addr, Username: username, Token: token, Style: style, Quiet: true, LastSeen: 0}
	if err := store.Save(cfg); err != nil {
		return err
	}
	fmt.Println("You're signed in. Run `sockt` to chat.")
	return nil
}
func usage() {
	fmt.Println(`Sockt v0.7 — terminal messenger

  sockt                  Open chat (first launch prompts for account setup)
  sockt setup            Register or log into a different server/account
  sockt login            Renew a session using your account password
  sockt --style classic  Use the classic layout for this run
  sockt --server URL     Only accepts current configured URL (token safety)
  sockt version          Print version

Remote connections REQUIRE wss:// via a trusted HTTPS reverse proxy.
Only ws://localhost... is permitted for local development.`)
}
func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			usage()
			return
		case "version":
			fmt.Println("sockt v0.7.0")
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
	if len(args) > 0 && (args[0] == "setup" || args[0] == "login") {
		if err := onboard(store, args[0] == "login"); err != nil {
			fmt.Fprintln(os.Stderr, "Account:", err)
			os.Exit(1)
		}
		return
	}
	if store.Value.Token == "" || store.Value.Server == "" {
		fmt.Println("Sockt needs an account before chatting.")
		if err := onboard(store, false); err != nil {
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
	style := flags.String("style", store.Value.Style, "UI appearance: minimal or classic")
	quiet := flags.Bool("quiet", store.Value.Quiet, "Suppress nonessential notices")
	_ = flags.Parse(args)
	if *style != "minimal" && *style != "classic" {
		fmt.Fprintln(os.Stderr, "Style must be minimal or classic")
		os.Exit(2)
	}
	if err := config.ValidateServerAddress(*addr); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid server:", err)
		os.Exit(2)
	}
	if *addr != store.Value.Server {
		fmt.Fprintln(os.Stderr, "Cannot reuse a saved session token with a different server URL. Run `sockt setup` and log into that server instead.")
		os.Exit(2)
	}
	since := store.Value.LastSeen
	onSeen := store.UpdateSeen
	session := client.New(*addr, store.Value.Username, store.Value.Token, since, onSeen)
	defer session.Close()
	program := tea.NewProgram(tui.New(store.Value.Username, session, *style, *quiet))
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Sockt UI:", err)
		os.Exit(1)
	}
}
