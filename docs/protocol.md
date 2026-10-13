# Sockt v0.7 wire protocol

Transport: **WSS** in remote deployments; **WS only on loopback** for local development. The HTTPS reverse proxy upgrades requests for `/ws` and forwards them to `socktd`. Sockt uses coder/websocket's `NetConn` to carry the unchanged v0.6 newline-delimited JSON protocol inside WebSocket text frames. Maximum one JSON packet: 256 KiB; maximum message body: 2048 bytes. WebSocket compression is disabled.

Handshake (first packet): `{"version":2,"type":"register","username":"...","invite":"...","password":"..."}`, `password_login` with password, or `login` with username, session token and `since_id`. Account requests receive `authenticated` (token) or `error` then close. Chat login receives zero/more `history` batches, `ready`, then async `presence` and `message` events. Client sends `chat` and `ping`; server sends `message`, `pong`, `presence`, `error`.

Version is deliberately 2 because the **application JSON schema** is backward compatible, while the transport is intentionally switched from raw TCP to WebSockets: v0.6 TCP binaries cannot connect to the v0.7 WebSocket endpoint.

The server ignores client-supplied username on `chat` and uses the user identity associated with the authenticated session. History and events currently represent one shared general room. Friends, DMs, groups and replies are future protocol extensions; never allow private-room access without server-enforced membership checks.

## Account-bound admin extension (version 2)

The chat `ready` packet optionally includes `"admin":true` for the configured
admin account. Omission means false, so old clients ignore the extension and new
clients hide admin commands when connected to older servers. Capability is not
persisted; clients clear it on disconnect, logout, or failed authentication.

On an authenticated chat connection, clients can send:

```json
{"type":"admin_status"}
{"type":"admin_invite_create","target":"Guest","confirmed":true}
{"type":"admin_invite_revoke","target":"Guest","confirmed":true}
```

Mutations require a valid username and explicit confirmation. Revoke removes all
unused invites for that username. The server reauthenticates using the original
login session and authorizes its immutable database account UUID on **every**
request, ignoring any identity, token, or capability included in the request.
Admin requests as the initial handshake are not accepted.

Responses use `admin_result` or `admin_error`, with `action` naming the request.
Status results contain `admin_status` with `version`, `commit`, `uptime_seconds`,
`connections`, and `accounts`. Create returns the invite secret once in `invite`;
revoke returns a count in `text`. Errors contain fixed messages and no credentials
or database errors. The existing dedicated socket writer delivers these results
alongside normal chat events; no separate connection or role credential is used.
