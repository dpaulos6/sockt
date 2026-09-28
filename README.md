# Sockt v0.9 — invitation-only terminal messenger

Sockt is a compact Go/Bubble Tea terminal chat app. The Go server runs behind a
trusted HTTPS reverse proxy, accepts authenticated WebSocket connections and
stores accounts, sessions and messages in PostgreSQL. v0.9 adds self-service
account recovery and management while keeping the app **invitation-only**, with
**no mandatory email**.

## New in v0.9

- New users register with a username-specific one-time invitation and password.
  Registration creates **eight single-use recovery codes** to store privately.
- Existing users can generate recovery codes with `sockt account` and their
  current password; generating new codes invalidates all earlier ones.
- Forgot your password? `sockt recover` uses a single unused recovery code to
  reset it, revokes every old session and issues a new recovery-code set.
- Manage active device sessions, revoke any session, change passwords, or log
  out the current device. Two devices can now chat using the **same username**.
  Presence counts unique usernames rather than connected sessions.
- Press `Ctrl+P`, or type `/account`, to open the account-management menu from
  the chat UI. The menu uses secure terminal password prompts outside the TUI.
- Preserves the signed, opt-in v0.8 client updater and existing chat protocol.
  There is no database modification to message content or chat-history IDs.

## Upgrading a running server

**Do not deploy the v0.9 client before the v0.9 server.** The server must first
apply the additive Neon PostgreSQL migration `002_accounts.up.sql`. It preserves
existing users, passwords, messages, invitations and sessions. Read the full
**[v0.9 Hetzner/Neon/PM2 deployment guide](docs/ACCOUNTS-v09.md)** and
[the short upgrade overview](UPGRADE-v08.md) before updating production.

Keep your current v0.8 Ed25519 release signing key. **Don't regenerate it** when
publishing the v0.9 client. The existing Nginx routes at `/ws` and `/updates`
remain unchanged. For client update publishing, see
[the updater guide](docs/UPDATES.md).

## Main commands

```text
sockt                 Launch the chat
sockt setup           Register, log in, or recover an account
sockt login           Login with username and password
sockt recover         Reset a forgotten password with one unused recovery code
sockt account         Sessions, revoke, password change, recovery-code rotation
sockt logout          Log out and revoke this device's session
sockt version         Print the client version
socktd migrate        Apply SQL migrations (operator, on server)
socktd invite USER    Create a single-use, username-bound invitation
socktd serve          Start the WebSocket server (PM2 on Hetzner)
```

## Local development

Install the Go version specified by `go.mod` and Docker Compose if using the
bundled local PostgreSQL container. The production database belongs in Neon
and must not be used for automated integration tests.

```bash
# From the repository root. Configure .env with your local database password.
docker compose up -d
go mod tidy
go vet ./...
go test ./...
go build ./...
# Set DATABASE_URL in your shell or protected .env loader, then:
go run ./cmd/socktd migrate
go run ./cmd/socktd invite TestUser
go run ./cmd/socktd serve
```

Run `go run ./cmd/sockt setup` in another terminal with
`ws://127.0.0.1:8080/ws` to test local registration. The new-client default is
`wss://chat.dpaulos.pt/ws` for the hosted beta, but the setup prompt lets you
enter a different secure server address.

For PostgreSQL integration tests, create an **isolated disposable database**
and set `SOCKT_TEST_DATABASE_URL`. Never set that variable to your real Neon
production branch: the test suite deliberately creates example accounts.

## Scope and security

v0.9 has one shared general conversation. Friends, DMs, groups, optional
verified email, password-reset emails, MFA, and OS credential-vault integration
remain future work. Tokens are currently saved in a user-only local config
file. Passwords are Argon2id-hashed; server-side recovery codes are only hashed
and single-use. Public remote connections must use `wss://`. Sockt does **not**
provide end-to-end encryption: the server and database administrator can access
stored messages. Limit invitations to trusted users while this is a beta.
