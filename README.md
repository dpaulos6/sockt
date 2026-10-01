# Sockt

Sockt is an invitation-only terminal messenger written in Go. The Bubble Tea v2
client connects over WebSocket to a small Go server backed by PostgreSQL. The
current stable release is `v0.9.2`.

## Features

- Portable Windows amd64 and Linux amd64/arm64 terminal clients.
- Account registration using username-bound invitations, passwords, sessions,
  recovery codes, device management, and logout.
- Shared real-time chat, history replay, reconnects, slash-command completion,
  and signed opt-in client updates.
- A loopback-only server designed for TLS termination by Nginx.

## Install

GitHub Releases are the canonical immutable distribution source. The rendered
release installer verifies a signed checksum file before installing binaries.
It needs neither Go nor administrator privileges.

Windows PowerShell 7.4+:

```powershell
irm https://github.com/dpaulos6/sockt/releases/download/v<VERSION>/install-sockt.ps1 | iex
```

Linux (requires `curl`, `openssl`, and `xxd`):

```bash
curl -fsSLO https://github.com/dpaulos6/sockt/releases/download/v<VERSION>/install-sockt.sh
bash install-sockt.sh
```

Open a new terminal if needed, then run `sockt`. Windows installs both
`sockt.exe` and its companion `sockt-updater.exe` in a user-owned directory.
Installers preserve account/session configuration. Use `sockt update` for the
existing signed in-app update path and `sockt doctor` for local diagnostics.

## Usage

```text
sockt setup          Register, log in, or recover access
sockt                Open chat
sockt account        Manage sessions, passwords, and recovery codes
sockt update         Install an available signed update
sockt doctor         Check local installation readiness
sockt version        Print build version and commit
```

Registration requires an invitation created by a server operator with
`socktd invite USER`. Public connections require `wss://`.

## Building and local development

Install the Go version from `go.mod`. Docker Compose is optional and exists
only for a disposable local PostgreSQL database; production uses Neon, Nginx,
and PM2. See [development](docs/DEVELOPMENT.md).

```bash
go fmt ./...
go vet ./...
go test -race ./...
go build ./...
```

## Security and limitations

Sockt is not end-to-end encrypted: server operators and the database can read
stored messages. Never share recovery codes, session configuration, invitation
tokens, or signing keys. Signed update manifests use the existing Ed25519 key;
do not replace it. Read [SECURITY.md](SECURITY.md) before reporting issues.

## Operations and contributing

- [Architecture](docs/architecture.md)
- [Protocol](docs/protocol.md)
- [Release process](docs/RELEASING.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Disaster recovery](docs/DISASTER_RECOVERY.md)
- [Contributing](CONTRIBUTING.md)

Docker is not part of the production deployment path. A license has not yet
been selected; choose and add one before making the repository public.