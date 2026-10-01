#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist
source "$(dirname "$0")/validate-release.sh"
# Leave unset for local development; production releases MUST use the stable
# public key generated once using `sockt-release keygen`.
release_key="${SOCKT_UPDATE_PUBLIC_KEY:-}"
release_version="${SOCKT_RELEASE_VERSION:-dev}"
release_commit="${SOCKT_RELEASE_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
[[ $release_commit =~ ^[0-9a-f]{7,40}$ || $release_commit == unknown ]] || { echo 'invalid commit' >&2; exit 2; }
if [[ $release_version != dev ]]; then
  RELEASE_TAG="v${release_version#v}"; validate_release; validate_key
  release_version=$RELEASE_TAG
elif [[ -n $release_key ]]; then validate_key; fi
if [[ "${SOCKT_REQUIRE_TAG:-0}" == 1 ]]; then
  RELEASE_TAG=$release_version; validate_release; validate_key
  RELEASE_COMMIT=$release_commit; validate_commit
  git cat-file -e "$release_version^{tag}"
  test "$(git describe --exact-match --tags)" = "$release_version" || { echo "release tag does not match requested version" >&2; exit 2; }
  [[ $(git rev-parse HEAD) == "$release_commit" ]] || { echo 'commit mismatch' >&2; exit 2; }
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
