# Run from the folder containing go.mod, using PowerShell.
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force dist | Out-Null
$releaseKey = $env:SOCKT_UPDATE_PUBLIC_KEY
$releaseVersion = if ($env:SOCKT_RELEASE_VERSION) { $env:SOCKT_RELEASE_VERSION } else { "dev" }
$releaseCommit = if ($env:SOCKT_RELEASE_COMMIT) { $env:SOCKT_RELEASE_COMMIT } else { (git rev-parse --short=12 HEAD) }
if ($env:SOCKT_REQUIRE_TAG -eq "1") { if ($releaseVersion -notmatch "^v?\d+\.\d+\.\d+$" -or (git describe --exact-match --tags) -ne $releaseVersion) { throw "release version must match the current annotated tag" } }
$ldflags = "-s -w -X sockt/internal/updater.PublicKeyHex=$releaseKey -X sockt/internal/buildinfo.Version=$releaseVersion -X sockt/internal/buildinfo.Commit=$releaseCommit"
$env:CGO_ENABLED = "0"
try {
    $env:GOOS = "windows"; $env:GOARCH = "amd64"
    go build -trimpath -ldflags="$ldflags" -o dist/sockt-windows-amd64.exe ./cmd/sockt
    if ($LASTEXITCODE -ne 0) { throw "Windows client build failed" }
    go build -trimpath -ldflags="-s -w" -o dist/sockt-updater-windows-amd64.exe ./cmd/sockt-updater
    if ($LASTEXITCODE -ne 0) { throw "Windows updater build failed" }
    $env:GOOS = "linux"; $env:GOARCH = "amd64"
    go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-amd64 ./cmd/sockt
    if ($LASTEXITCODE -ne 0) { throw "Linux client build failed" }
    go build -trimpath -ldflags="$ldflags" -o dist/socktd-linux-amd64 ./cmd/socktd
    if ($LASTEXITCODE -ne 0) { throw "Linux server build failed" }
    $env:GOARCH = "arm64"
    go build -trimpath -ldflags="$ldflags" -o dist/sockt-linux-arm64 ./cmd/sockt
    if ($LASTEXITCODE -ne 0) { throw "Linux arm64 client build failed" }
    go build -trimpath -ldflags="$ldflags" -o dist/socktd-linux-arm64 ./cmd/socktd
    if ($LASTEXITCODE -ne 0) { throw "Linux arm64 server build failed" }
}
finally {
    Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
if (-not $releaseKey) { Write-Warning "SOCKT_UPDATE_PUBLIC_KEY is unset; built clients will NOT check for updates." }
Write-Host "Built binaries in dist/ (client release version: $releaseVersion)"
