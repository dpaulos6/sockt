# Dot-source this file from a PowerShell profile to add local Sockt helpers.
function sockt-test { go fmt ./...; go vet ./...; go test -race ./... }
function sockt-build { & "$PSScriptRoot/build.ps1" }
function sockt-status { git status --short --branch; git describe --tags --always }
function sockt-release {
    param([ValidateSet('patch','minor')][string]$Bump)
    if (-not $Bump) { throw 'This prepares a tag candidate only; it never deploys. Use sockt-release patch or minor.' }
    if ((git status --porcelain)) { throw 'Working tree must be clean.' }
    if ((git branch --show-current) -ne 'master') { throw 'Release preparation requires master.' }
    $current = (git describe --tags --abbrev=0).TrimStart('v').Split('.')
    if ($Bump -eq 'patch') { $current[2] = [int]$current[2] + 1 } else { $current[1] = [int]$current[1] + 1; $current[2] = 0 }
    $next = 'v' + ($current -join '.')
    Write-Host "Prepared candidate $next. Review, create an annotated tag, push it, then manually dispatch Release."
}
function sockt-rollback { Write-Warning 'No local rollback is performed. Follow docs/DISASTER_RECOVERY.md and the approved Hetzner runbook.' }
function sockt-deployment-status {
    param([Parameter(Mandatory)][string]$Host, [string]$User = 'socktdeploy')
    ssh "$User@$Host" 'sudo /usr/local/sbin/sockt-deploy status'
}
function sockt-deployment-rollback {
    param([Parameter(Mandatory)][string]$Host, [string]$User = 'socktdeploy', [switch]$DryRun)
    $mode = if ($DryRun) { ' --dry-run' } else { '' }
    Write-Warning 'This changes the server executable, not the database schema.'
    ssh "$User@$Host" "sudo /usr/local/sbin/sockt-deploy rollback$mode"
}
function sockt-release-run {
    param([Parameter(Mandatory)][string]$Version, [switch]$ApplyMigrations, [string]$BackupId)
    if ($Version -notmatch '^v\d+\.\d+\.\d+$') { throw 'Version must be an annotated vX.Y.Z tag.' }
    gh workflow run release.yml --ref $Version -f version=$Version -f apply_migrations=$($ApplyMigrations.IsPresent.ToString().ToLower()) -f backup_id=$BackupId
}
