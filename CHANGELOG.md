## Unreleased — Persistent Account view

- Keep one Bubble Tea application and chat connection across Chat/Account navigation.
- Add asynchronous device listing, confirmed revocation/logout, hidden password
  entry, and recovery-code replacement with explicit acknowledgment.
- Preserve drafts and history while Account receives background connection and
  update events; reconnect only when a password change replaces the session.
- Serialize token persistence with history saves and reject stale transport events.
- Keep standalone CLI account commands and the signed-update protocol unchanged.

## v0.9.2 — Windows updater restart reliability

- Restart the client in its existing PowerShell or Windows Terminal console after an update.
- Open Windows console input and output read/write so Bubble Tea v2 can enable raw input mode.
- v0.9.2 also recovers when the v0.9.1 updater supplied a read-only console input handle.

## v0.9.0 — Accounts and recovery

- One-time, hash-stored recovery codes shown only once on registration/rotation.
- Username + recovery-code password reset revokes all prior device sessions.
- Self-service password changes, log out, list/revoke active devices.
- Multiple simultaneous chat sessions for the same account (unique-user presence).
- New `sockt account`, `sockt recover`, `sockt logout` and `/account` / Ctrl+P.
- Additive Neon PostgreSQL migration; optional email columns reserved, no SMTP.
- Per-username password/recovery attempt limits; active-session revocation.
- Defaults for new client installations use wss://chat.dpaulos.pt/ws.
- v0.8's signed client updater remains in place; reuse signing key for releases.

## v0.8 — Signed in-app client updates

- Optional signed client update checks at startup and every 30 minutes via the current WSS host.
- Terminal update banner; F2 then Y downloads, verifies and restarts the client.
- Ed25519 manifest authentication and SHA-256 binaries, HTTPS-only same-host updates.
- Companion Windows updater helper for locked running executables; Linux exec-based restart.
- Offline release signing tool and static Nginx deployment guide.
- No PostgreSQL schema change; deployed server binary remains v0.7 compatible.

# Changelog

## v0.7.0 — WebSockets over HTTPS
- Convert the client network transport and account setup requests from raw TCP to WebSocket, using `github.com/coder/websocket`.
- Add HTTP `/ws` endpoint on **loopback only**, plus `/healthz`. Require **wss://** for remote client URLs.
- Keep PostgreSQL schema, invites/password login, 30-day sessions, async shared chat, history replay, automatic reconnect and minimal/classic Bubble Tea UI.
- Add Nginx and Caddy separate-subdomain examples compatible with a VPS also hosting Next.js.
- Update systemd, Hetzner instructions, URL validation, build tests and upgrade guidance.
- No friends/DMs/groups/replies yet. Messages remain server-accessible (not E2EE).

## v0.6.0 — PostgreSQL and accounts
- PostgreSQL schema and migrations, invite-only accounts, hashed passwords and persistent shared chat.
