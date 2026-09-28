# Run from the folder containing go.mod, using PowerShell.
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/sockt-windows-amd64.exe ./cmd/sockt
if ($LASTEXITCODE -ne 0) { throw "Windows build failed" }
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/socktd-linux-amd64 ./cmd/socktd
if ($LASTEXITCODE -ne 0) { throw "Linux amd64 build failed" }
$env:GOARCH = "arm64"
go build -trimpath -ldflags="-s -w" -o dist/socktd-linux-arm64 ./cmd/socktd
if ($LASTEXITCODE -ne 0) { throw "Linux arm64 build failed" }
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED -ErrorAction SilentlyContinue
Write-Host "Built binaries in dist/"
