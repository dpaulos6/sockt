# Rendered release installer. Windows PowerShell 5.1 / PowerShell 7 + OpenSSL 3.
[CmdletBinding()]
param(
    [ValidateSet('Install','Update','Reinstall','Uninstall')][string]$Action = 'Install',
    [string]$Version = '__SOCKT_VERSION__',
    [string]$Repository = ''
)
$ErrorActionPreference = 'Stop'
$PublicKey = '__SOCKT_PUBLIC_KEY__'
function Get-SocktTag([string]$Value) {
    if ($Value -cnotmatch '^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') { throw 'Invalid release version' }
    return 'v' + ($Value -replace '^v','')
}
function Get-SocktReleaseBase([string]$Tag,[string]$Repo) {
    $Tag=Get-SocktTag $Tag
    if (-not $Repo) { return "https://chat.dpaulos.pt/updates/releases/$Tag" }
    if ($Repo -cnotmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { throw 'Invalid repository' }
    return "https://github.com/$Repo/releases/download/$Tag"
}
function Convert-SocktHex([string]$Hex) {
    if ($Hex -cnotmatch '^(?:[0-9a-fA-F]{2})+$') { throw 'Invalid hex' }
    $bytes = New-Object byte[] ($Hex.Length / 2)
    for ($i=0; $i -lt $bytes.Length; $i++) { $bytes[$i]=[Convert]::ToByte($Hex.Substring($i*2,2),16) }
    return ,$bytes
}
function Test-SocktSignature([string]$Payload,[string]$Signature,[string]$Key,[string]$Work) {
    if ($Key -cnotmatch '^[0-9a-fA-F]{64}$' -or $Key -match '^0+$') { throw 'Invalid public key' }
    if ((Get-Item -LiteralPath $Signature).Length -ne 64) { throw 'Signature must be 64 raw bytes' }
    $openssl = (Get-Command openssl -CommandType Application -ErrorAction Stop).Source
    $opensslVersion = & $openssl version
    if ($LASTEXITCODE -ne 0 -or $opensslVersion -notmatch '^OpenSSL 3\.') { throw 'OpenSSL 3 is required; install it from a trusted source' }
    $der = Join-Path $Work 'public.der'
    [IO.File]::WriteAllBytes($der,(Convert-SocktHex ('302a300506032b6570032100'+$Key)))
    & $openssl pkeyutl -verify -pubin -inkey $der -keyform DER -rawin -in $Payload -sigfile $Signature *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Release signature verification failed' }
}
function Get-SocktChecksum([string]$File,[string]$Name) {
    $matches = @(Get-Content -LiteralPath $File | Where-Object { $_ -cmatch ('^[0-9a-f]{64}  '+[regex]::Escape($Name)+'$') })
    if ($matches.Count -ne 1) { throw 'Missing or duplicate checksum entry' }
    return $matches[0].Substring(0,64)
}
function Test-SocktBinary([string]$File,[string]$Checksums,[string]$Name) {
    $expected = Get-SocktChecksum $Checksums $Name
    # Framework API works in 5.1 and 7 even when a caller's PSModulePath does not
    # expose the Get-FileHash module. Hash the stream without loading the binary.
    $stream = [IO.File]::OpenRead($File)
    $hasher = [Security.Cryptography.SHA256]::Create()
    try { $actual = [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-','').ToLowerInvariant() }
    finally { $stream.Dispose(); $hasher.Dispose() }
    if ($actual -cne $expected) { throw 'Binary checksum verification failed' }
}
function Get-SocktInstallDirectory([string]$LocalAppData) {
    # v0.9 installations live here; do not introduce a competing bin directory.
    return Join-Path $LocalAppData 'Sockt'
}
function Install-SocktFiles([string]$work,[string]$Bin,[string]$OldBin) {
    New-Item -ItemType Directory -Force $Bin | Out-Null
    $changed = @()
    try {
        foreach ($name in @('sockt-updater.exe','sockt.exe')) {
            $dest = Join-Path $Bin $name
            $backup = Join-Path $Bin ($name+'.installer-previous')
            if (Test-Path -LiteralPath $dest) { Copy-Item -LiteralPath $dest -Destination $backup -Force }
            $staged = Join-Path $Bin ($name+'.installer-next')
            Copy-Item -LiteralPath (Join-Path $work $name) -Destination $staged -Force
            Move-Item -LiteralPath $staged -Destination $dest -Force
            $changed += $name
        }
        # Remove the unreleased bin-layout duplicates only after both replacements.
        foreach ($name in @('sockt.exe','sockt-updater.exe')) { Remove-Item -LiteralPath (Join-Path $OldBin $name) -Force -ErrorAction SilentlyContinue }
    } catch {
        foreach ($name in $changed) {
            $dest=Join-Path $Bin $name; $backup=Join-Path $Bin ($name+'.installer-previous')
            if (Test-Path -LiteralPath $backup) { Copy-Item -LiteralPath $backup -Destination $dest -Force } else { Remove-Item -LiteralPath $dest -Force }
        }
        throw
    }
}
$Bin = Get-SocktInstallDirectory $env:LOCALAPPDATA
$OldBin = Join-Path $Bin 'bin'
function Update-SocktPath([bool]$Remove) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $items = @($current -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ine $Bin.TrimEnd('\') -and $_.TrimEnd('\') -ine $OldBin.TrimEnd('\') })
    if (-not $Remove) { $items = @($Bin) + $items }
    [Environment]::SetEnvironmentVariable('Path', ($items -join ';'), 'User')
    # Preserve the current process PATH (including session-only tools).
    $processItems = @($env:Path -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ine $Bin.TrimEnd('\') -and $_.TrimEnd('\') -ine $OldBin.TrimEnd('\') })
    if (-not $Remove) { $processItems = @($Bin) + $processItems }
    $env:Path = $processItems -join ';'
}
# Abort rather than replacing a running executable/helper or following junctions.
foreach ($directory in @($Bin,$OldBin)) {
    if ((Test-Path -LiteralPath $directory) -and ((Get-Item -LiteralPath $directory).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Install directory must not be a junction or symlink' }
}
foreach ($process in @(Get-Process sockt,sockt-updater -ErrorAction SilentlyContinue)) {
    if ($process.Path -and ($process.Path.StartsWith($Bin+'\',[StringComparison]::OrdinalIgnoreCase))) { throw 'Close Sockt before installing or removing it' }
}
if ($Action -eq 'Uninstall') {
    foreach ($dir in @($Bin,$OldBin)) {
        foreach ($name in @('sockt.exe','sockt-updater.exe')) { Remove-Item -LiteralPath (Join-Path $dir $name) -Force -ErrorAction SilentlyContinue }
    }
    Update-SocktPath $true
    Write-Host 'Sockt binaries removed; account configuration preserved.'
    return
}
$tag = Get-SocktTag $Version
if ($PublicKey -cnotmatch '^[0-9a-fA-F]{64}$') { throw 'Unrendered or invalid public key' }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$base = Get-SocktReleaseBase $tag $Repository
$work = Join-Path ([IO.Path]::GetTempPath()) ('sockt-install-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    $checksums = Join-Path $work 'checksums'
    $signature = Join-Path $work 'checksums.sig'
    Invoke-WebRequest -UseBasicParsing "$base/sockt-$tag-checksums.txt" -OutFile $checksums
    Invoke-WebRequest -UseBasicParsing "$base/sockt-$tag-checksums.txt.sig" -OutFile $signature
    Test-SocktSignature $checksums $signature $PublicKey $work
    $names = @{'sockt.exe'='sockt-windows-amd64.exe';'sockt-updater.exe'='sockt-updater-windows-amd64.exe'}
    # Download/verify BOTH before changing either installed executable.
    foreach ($name in $names.Keys) {
        $download = Join-Path $work $name
        Invoke-WebRequest -UseBasicParsing "$base/$($names[$name])" -OutFile $download
        Test-SocktBinary $download $checksums $names[$name]
    }
    Install-SocktFiles $work $Bin $OldBin
    Update-SocktPath $false
    Write-Host "Sockt $tag installed. Run the verified executable: $Bin\sockt.exe"
    $resolved = Get-Command sockt -ErrorAction SilentlyContinue
    if ($resolved -and $resolved.Source -ine (Join-Path $Bin 'sockt.exe')) { Write-Warning 'A shell alias/function or another installation shadows sockt. Use the full path above.' }
} finally {
    # Only this invocation's GUID directory under the resolved temp root is removed.
    $resolvedWork=[IO.Path]::GetFullPath($work)
    $tempRoot=[IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\')+'\'
    if ($resolvedWork.StartsWith($tempRoot,[StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $resolvedWork -Recurse -Force }
}
