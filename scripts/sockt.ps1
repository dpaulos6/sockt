# Dot-source this file from a PowerShell profile to add local Sockt helpers.
function sockt-test { go fmt ./...; go vet ./...; go test -race ./... }
function sockt-build { & "$PSScriptRoot/build.ps1" }
function sockt-status { git status --short --branch; git describe --tags --always }
function sockt-release {
    param([ValidateSet('patch','minor')][string]$Bump)
    if (-not $Bump) { throw 'This prepares a tag candidate only; it never deploys. Use sockt-release patch or minor.' }
    if ((git status --porcelain)) { throw 'Working tree must be clean.' }
    if ((git branch --show-current) -ne 'master') { throw 'Release preparation requires master.' }
    $tag = git describe --tags --abbrev=0 --match 'v[0-9]*'
    if ($LASTEXITCODE -ne 0 -or -not $tag) { throw 'No release baseline. Verify deployed v0.9.2, then follow docs/RELEASING.md to annotate its exact commit. No tag was created.' }
    if ($tag -cnotmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') { throw 'Invalid baseline tag' }
    $current = $tag.Substring(1).Split('.')
    if ($Bump -eq 'patch') { $current[2] = [int]$current[2] + 1 } else { $current[1] = [int]$current[1] + 1; $current[2] = 0 }
    $next = 'v' + ($current -join '.')
    Write-Host "Prepared candidate $next. Review, create an annotated tag, push it, then manually dispatch Release."
}
function sockt-rollback { Write-Warning 'No local rollback is performed. Follow docs/DISASTER_RECOVERY.md and the approved Hetzner runbook.' }
function sockt-deployment-status {
    param([Parameter(Mandatory)][string]$DeployHost, [string]$User = 'socktdeploy')
    if ($User -cnotmatch '^[a-z_][a-z0-9_-]{0,31}$' -or $DeployHost -cnotmatch '^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$') { throw 'Invalid SSH destination' }
    ssh "$User@$DeployHost" 'sudo /usr/local/sbin/sockt-deploy status'
}
function sockt-deployment-rollback {
    param([Parameter(Mandatory)][string]$DeployHost, [string]$User = 'socktdeploy', [switch]$DryRun)
    $mode = if ($DryRun) { ' --dry-run' } else { '' }
    Write-Warning 'This changes the server executable, not the database schema.'
    if ($User -cnotmatch '^[a-z_][a-z0-9_-]{0,31}$' -or $DeployHost -cnotmatch '^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$') { throw 'Invalid SSH destination' }
    ssh "$User@$DeployHost" "sudo /usr/local/sbin/sockt-deploy rollback$mode"
}
function sockt-release-run {
    param([Parameter(Mandatory)][string]$Version, [switch]$ApplyMigrations, [string]$BackupId)
    if ($Version -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') { throw 'Version must be an annotated vX.Y.Z tag.' }
    if ($BackupId -and $BackupId -cnotmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$') { throw 'Invalid backup ID' }
    if ($ApplyMigrations -and -not $BackupId) { throw 'Backup receipt required' }
    gh workflow run release.yml --ref master -f version=$Version -f apply_migrations=$($ApplyMigrations.IsPresent.ToString().ToLower()) -f backup_id=$BackupId
}
