# Development

Install the Go version in `go.mod`. Production uses Neon; never point tests at
production. For a disposable local PostgreSQL instance, `docker compose up -d`
is optional. Set `SOCKT_TEST_DATABASE_URL` only to an isolated test database.

Run `go fmt ./...`, `go vet ./...`, and `go test -race ./...`. Development
builds intentionally have no embedded update public key, so signed updates are
disabled. Use `sockt doctor` to inspect local installation readiness.
