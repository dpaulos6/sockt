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
