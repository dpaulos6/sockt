# v0.7 release verification checklist

This source archive contains production-intended Go code and runnable test cases, not compiled binaries. **Do not invite friends until the commands below pass on a machine that can actually download the declared Go dependencies and reach PostgreSQL.**

## Local Go tests

```bash
go version  # Go 1.25 or newer
go mod tidy  # fetch genuine github.com/coder/websocket + existing v0.6 dependencies
gofmt -l .  # should print nothing
go vet ./...
go test -race -count=1 ./...
go build ./...
```

Critical test coverage in this archive:
- `internal/config`: WSS-only remote URL validation, safe legacy client-config migrations.
- `internal/transport`: actual coder/websocket round-trip (when using real modules).
- `internal/server`: WebSocket signup/login, broadcasting, history; plus original transport-independent concurrent/hub tests.
- `internal/database`: original v0.6 tests; integration tests need an isolated PostgreSQL service and `SOCKT_TEST_DATABASE_URL`.

## Application tests before Hetzner deployment

1. Start the local Docker PostgreSQL, set `DATABASE_URL`, run migrations and create two distinct invites.
2. Start `socktd serve`, verify `curl http://127.0.0.1:8080/healthz` returns `ok`.
3. Register two distinct OS-user profiles through `ws://127.0.0.1:8080/ws`; send and receive in both directions.
4. Stop/restart server. Ensure each client reconnects and missed messages are replayed without duplicates. Confirm canonical timestamps come from PostgreSQL.
5. Confirm `ws://<non-loopback>/ws` is refused by the CLI and `socktd --listen 0.0.0.0:8080` refuses to start (with DB configured).
6. Test your deployed `wss://chat.yourdomain.com/ws` from an **independent network** after TLS validation. A bad/expired certificate must cause a connection error.
7. Confirm correct behavior on browser origin mismatch and proxy upgrade headers with the actual dependencies in a staging environment.
8. Reboot/restart Hetzner service, verify existing Next.js site still works and Sockt is reachable over WSS, check off-server database backup and trial restore.

## Environment limitation of the generated archive

Its source layout and syntax were checked and the app's existing server logic, WebSocket-adapter API surface, config/protocol tests and race tests were exercised in an offline environment with **test-only replacements** for unavailable Go modules. Those replacements emulate the WebSocket upgrade using a raw TCP tunnel, **not genuine RFC6455 frames**, and do not implement actual Bubble Tea, PostgreSQL or cryptography. The release ZIP does **not** include those replacements. Genuine `go mod tidy`, complete real-dependency test execution, real PostgreSQL integration and end-to-end HTTPS/WSS acceptance **remain your deployment gates**, not achievements claimed by the offline checks.
