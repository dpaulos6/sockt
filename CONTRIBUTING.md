# Contributing to Sockt

Use a feature branch and pull request. Run `go fmt ./...`, `go vet ./...`, and
`go test -race ./...` before requesting review. Never commit binaries, account
configuration, database URLs, recovery codes, signing material, or production
server configuration. Changes under `.github/`, `scripts/`, and `deploy/`
require release/security review.

Sockt is a Go terminal client and server; preserve the existing Bubble Tea,
WebSocket, PostgreSQL, and signed-update architecture.
