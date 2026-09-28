# Sockt v0.7 wire protocol

Transport: **WSS** in remote deployments; **WS only on loopback** for local development. The HTTPS reverse proxy upgrades requests for `/ws` and forwards them to `socktd`. Sockt uses coder/websocket's `NetConn` to carry the unchanged v0.6 newline-delimited JSON protocol inside WebSocket text frames. Maximum one JSON packet: 256 KiB; maximum message body: 2048 bytes. WebSocket compression is disabled.

Handshake (first packet): `{"version":2,"type":"register","username":"...","invite":"...","password":"..."}`, `password_login` with password, or `login` with username, session token and `since_id`. Account requests receive `authenticated` (token) or `error` then close. Chat login receives zero/more `history` batches, `ready`, then async `presence` and `message` events. Client sends `chat` and `ping`; server sends `message`, `pong`, `presence`, `error`.

Version is deliberately 2 because the **application JSON schema** is backward compatible, while the transport is intentionally switched from raw TCP to WebSockets: v0.6 TCP binaries cannot connect to the v0.7 WebSocket endpoint.

The server ignores client-supplied username on `chat` and uses the user identity associated with the authenticated session. History and events currently represent one shared general room. Friends, DMs, groups and replies are future protocol extensions; never allow private-room access without server-enforced membership checks.
