#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist
# Leave unset for local development; production releases MUST use the stable
# public key generated once using `sockt-release keygen`.
release_key="${SOCKT_UPDATE_PUBLIC_KEY:-}"
release_version="${SOCKT_RELEASE_VERSION:-dev}"
release_commit="${SOCKT_RELEASE_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
if [[ "${SOCKT_REQUIRE_TAG:-0}" == 1 ]]; then
  [[ "$release_version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid release version" >&2; exit 2; }
  test "$(git describe --exact-match --tags)" = "$release_version" || { echo "release tag does not match requested version" >&2; exit 2; }
fi
ldflags="-s -w -X sockt/internal/updater.PublicKeyHex=$release_key -X sockt/internal/buildinfo.Version=$release_version -X sockt/internal/buildinfo.Commit=$release_commit"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-windows-amd64.exe ./cmd/sockt
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/sockt-updater-windows-amd64.exe ./cmd/sockt-updater
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-amd64 ./cmd/sockt
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-arm64 ./cmd/sockt
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$ldflags" -o dist/socktd-linux-amd64 ./cmd/socktd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$ldflags" -o dist/socktd-linux-arm64 ./cmd/socktd
if [[ -z "$release_key" ]]; then echo 'WARNING: update public key unset; built clients will NOT check for updates.'; fi
printf 'Built binaries in dist/ (client release version: %s)\n' "$release_version"
