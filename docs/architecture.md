# Sockt v0.7 architecture

Modular Go monolith: the terminal client speaks encrypted WebSockets (`wss://.../ws`) to an HTTPS reverse proxy. The proxy forwards to `socktd` on loopback. The Sockt protocol still consists of newline-delimited JSON packets (inside text WebSocket frames via a `net.Conn` adapter). PostgreSQL is local to the server, not exposed to users.

```
sockt TUI -> client package -> transport.DialWS -> WSS -> reverse proxy (443)
  -> localhost HTTP/WebSocket -> transport.ServeWS -> server.AcceptConnection
  -> hub -> PostgreSQL store
```

- `cmd`: client and server executable entry points (thin composition only).
- `internal/transport`: WebSocket dial/upgrade and byte-stream adapter; never contains business logic.
- `internal/protocol`: bounded line-delimited JSON messages, identical payload shape to v0.6.
- `internal/client`: connection lifecycle, login handshakes, reconnect/backoff, offline history cursor.
- `internal/server`: authenticated session handling, concurrency, rate limits, broadcasts.
- `internal/database`: SQL storage, migrations, Argon2id password hashes and token hashes.
- `internal/tui`: Bubble Tea minimal/classic UI with separate input composer.
- `internal/config`: user-only session config, secure URL validation.

The single general room is still the only supported conversation. The database models users, conversation members and messages for subsequent friend/DM/group work. Messages are stored server-side in plaintext; WSS is **transport encryption**, not E2EE. v0.7 server refuses public/nonloopback binds so TLS cannot be accidentally bypassed on the public interface.

The normal CLI reconnects over WSS and recovers messages using `SinceID`; the database assigns canonical message IDs/timestamps before broadcasts. Incoming packets are size-bounded by the JSON scanner; WebSocket compression is disabled. Standard origin checks remain enabled at WS upgrade, and no session token is placed in URLs, request headers or proxy access logs by Sockt. Configure your proxy not to log message bodies or credential-bearing packets.
