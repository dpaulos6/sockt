#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist
# Leave unset for local development; production releases MUST use the stable
# public key generated once using `sockt-release keygen`.
release_key="${SOCKT_UPDATE_PUBLIC_KEY:-}"
release_version="${SOCKT_RELEASE_VERSION:-0.9.2}"
ldflags="-s -w -X sockt/internal/updater.PublicKeyHex=$release_key -X sockt/internal/updater.CurrentVersion=$release_version"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-windows-amd64.exe ./cmd/sockt
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/sockt-updater-windows-amd64.exe ./cmd/sockt-updater
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-amd64 ./cmd/sockt
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-arm64 ./cmd/sockt
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/socktd-linux-amd64 ./cmd/socktd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/socktd-linux-arm64 ./cmd/socktd
if [[ -z "$release_key" ]]; then echo 'WARNING: update public key unset; built clients will NOT check for updates.'; fi
printf 'Built binaries in dist/ (client release version: %s)\n' "$release_version"
