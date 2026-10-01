# Rendered by the protected release workflow. It verifies release binaries before install.
[CmdletBinding()]
param(
    [ValidateSet('Install','Update','Reinstall','Uninstall')][string]$Action = 'Install',
    [string]$Version = '__SOCKT_VERSION__',
    [string]$Repository = 'dpaulos6/sockt'
)
$ErrorActionPreference = 'Stop'
$PublicKey = '__SOCKT_PUBLIC_KEY__'
$Bin = Join-Path $env:LOCALAPPDATA 'Sockt\bin'
function Update-Path([bool]$Remove) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $items = @($current -split ';' | Where-Object { $_ -and $_ -ne $Bin })
    if (-not $Remove) { $items += $Bin }
    [Environment]::SetEnvironmentVariable('Path', ($items -join ';'), 'User')
    $env:Path = (($items -join ';') + ';' + [Environment]::GetEnvironmentVariable('Path', 'Machine'))
}
if ($Action -eq 'Uninstall') {
    Remove-Item -LiteralPath (Join-Path $Bin 'sockt.exe') -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $Bin 'sockt-updater.exe') -Force -ErrorAction SilentlyContinue
    Update-Path $true
    Write-Host 'Sockt binaries removed. Account configuration was preserved.'
    exit 0
}
if ($Version -match '^__SOCKT_' -or $PublicKey -match '^__SOCKT_') { throw 'This is an unrendered installer template.' }
$base = "https://github.com/$Repository/releases/download/v$Version"
$checksums = Join-Path $env:TEMP "sockt-$Version-checksums.txt"
$signature = "$checksums.sig"
Invoke-WebRequest "$base/sockt-v$Version-checksums.txt" -OutFile $checksums
Invoke-WebRequest "$base/sockt-v$Version-checksums.txt.sig" -OutFile $signature
if (-not ('System.Security.Cryptography.Ed25519' -as [type])) { throw 'PowerShell 7.4+/.NET 8+ is required to verify Sockt releases.' }
$ok = [System.Security.Cryptography.Ed25519]::Verify([Convert]::FromHexString($signature | Get-Content -Raw | ForEach-Object Trim), [IO.File]::ReadAllBytes($checksums), [Convert]::FromHexString($PublicKey))
if (-not $ok) { throw 'Release checksum signature verification failed.' }
New-Item -ItemType Directory -Force $Bin | Out-Null
foreach ($name in @('sockt-windows-amd64.exe','sockt-updater-windows-amd64.exe')) {
    $destination = if ($name -eq 'sockt-windows-amd64.exe') { Join-Path $Bin 'sockt.exe' } else { Join-Path $Bin 'sockt-updater.exe' }
    $download = Join-Path $env:TEMP "sockt-$Version-$name"
    Invoke-WebRequest "$base/$name" -OutFile $download
    $expected = ((Get-Content $checksums | Where-Object { $_ -match "\s$name$" }) -split '\s+')[0]
    if (-not $expected -or (Get-FileHash $download -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected.ToLowerInvariant()) { throw "Checksum failed for $name" }
    Move-Item -LiteralPath $download -Destination $destination -Force
}
Update-Path $false
Write-Host "Sockt $Version installed to $Bin. Open a new terminal, then run: sockt"
