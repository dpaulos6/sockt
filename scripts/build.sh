#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/sockt-windows-amd64.exe ./cmd/sockt
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/socktd-linux-amd64 ./cmd/socktd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/socktd-linux-arm64 ./cmd/socktd
printf 'Built binaries in dist/\n'
