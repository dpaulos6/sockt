param(
    [Parameter(Mandatory)][string]$Fixture,
    [Parameter(Mandatory)][ValidateSet(5,7)][int]$ExpectedMajor
)
$ErrorActionPreference='Stop'
if ($PSVersionTable.PSVersion.Major -ne $ExpectedMajor) { throw 'Wrong PowerShell runtime; requested version was not exercised' }
$tokens=$null; $parseErrors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'install.ps1'),[ref]$tokens,[ref]$parseErrors)
if ($parseErrors.Count) { throw $parseErrors[0] }
# Load the actual implementation without executing downloads or changing user PATH.
foreach ($fn in $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]},$false)) { Invoke-Expression $fn.Extent.Text }
function Assert-Throws([scriptblock]$Body) { $failed=$false; try { & $Body } catch { $failed=$true }; if (-not $failed) { throw 'Negative case unexpectedly succeeded' } }
$key=(Get-Content -LiteralPath (Join-Path $Fixture 'public.hex') -Raw).Trim()
$payload=Join-Path $Fixture 'sockt-v0.10.0-checksums.txt'
$sig=$payload+'.sig'
Test-SocktSignature $payload $sig $key $Fixture
Assert-Throws { Test-SocktSignature (Join-Path $Fixture 'tampered') $sig $key $Fixture }
Assert-Throws { Test-SocktSignature $payload (Join-Path $Fixture 'bad.sig') $key $Fixture }
Assert-Throws { Test-SocktSignature $payload $sig ('00'*32) $Fixture }
Test-SocktBinary (Join-Path $Fixture 'sockt-windows-amd64.exe') $payload 'sockt-windows-amd64.exe'
Assert-Throws { Test-SocktBinary (Join-Path $Fixture 'tampered') $payload 'sockt-windows-amd64.exe' }
Assert-Throws { Get-SocktChecksum $payload 'sockt-windows-amd64.exe.*' }
$duplicate=Join-Path $Fixture 'duplicate-checksums'
[IO.File]::WriteAllText($duplicate,([IO.File]::ReadAllText($payload)+[IO.File]::ReadAllText($payload)))
Assert-Throws { Get-SocktChecksum $duplicate 'sockt-windows-amd64.exe' }
if ((Get-SocktReleaseBase 'v0.10.0' '') -cne 'https://chat.dpaulos.pt/updates/releases/v0.10.0') { throw 'Invalid public release path' }
if ((Get-SocktReleaseBase '0.10.0' 'dpaulos6/sockt') -cne 'https://github.com/dpaulos6/sockt/releases/download/v0.10.0') { throw 'Invalid GitHub release path' }
foreach ($v in @('0.10.0','v0.10.0')) { if ((Get-SocktTag $v) -cne 'v0.10.0') { throw 'Version normalization failed' } }
foreach ($v in @('vv0.10.0','v01.2.3','v1.2.3;whoami',"v1.2.3`nboom",'../v1.2.3')) { Assert-Throws { Get-SocktTag $v } }
$local=Join-Path $Fixture 'localappdata'
$bin=Get-SocktInstallDirectory $local
if ($bin -ne (Join-Path $local 'Sockt')) { throw 'Legacy install directory changed' }
$old=Join-Path $bin 'bin'
New-Item -ItemType Directory -Force $old | Out-Null
Set-Content -LiteralPath (Join-Path $bin 'sockt.exe') -Value 'old client'
Set-Content -LiteralPath (Join-Path $old 'sockt.exe') -Value 'shadow client'
$stage=Join-Path $Fixture 'windows-stage'
New-Item -ItemType Directory -Force $stage | Out-Null
Set-Content -LiteralPath (Join-Path $stage 'sockt.exe') -Value 'new client'
Set-Content -LiteralPath (Join-Path $stage 'sockt-updater.exe') -Value 'new helper'
Install-SocktFiles $stage $bin $old
if ((Get-Content -LiteralPath (Join-Path $bin 'sockt.exe')) -ne 'new client' -or (Test-Path -LiteralPath (Join-Path $old 'sockt.exe'))) { throw 'Legacy update left a competing client' }
# Fail the second replacement and verify the first replacement is rolled back.
Remove-Item -LiteralPath (Join-Path $stage 'sockt.exe')
Set-Content -LiteralPath (Join-Path $stage 'sockt-updater.exe') -Value 'bad replacement'
Assert-Throws { Install-SocktFiles $stage $bin $old }
if ((Get-Content -LiteralPath (Join-Path $bin 'sockt-updater.exe')) -ne 'new helper') { throw 'Partial update did not roll back' }
Write-Output ('PASS PowerShell '+$PSVersionTable.PSVersion+' / runtime '+[Environment]::Version+'; Ed25519 .NET API present: '+[bool]('System.Security.Cryptography.Ed25519' -as [type]))
exit 0
