# Release CI repair and verification

## Root cause

`test_release.py` and `validate-release.sh` were implemented in the local working
tree but left untracked. No commit in their Git history contained them. Workflow
changes were committed separately, so GitHub runners correctly reported missing
files. The recovered implementation, PowerShell helper, deployment harness and Go
linker test must be committed together. Do not stage only the workflows.

## Test contracts

`scripts/validate-release.sh` can be sourced or executed. Executing it validates
`RELEASE_TAG`, `RELEASE_COMMIT`, `APPLY_MIGRATIONS` and `BACKUP_ID`. Sourced helpers
allow the workflow to validate the tag before Git resolves it, then require a full
40-character lowercase SHA before using the commit. Stable tags have exactly one
`v`, three nonnegative decimal parts and no leading zeros. Migration approval is
exactly `true` or `false`; approval requires a restricted receipt ID and an ID with
no approval is rejected. Additional helpers validate public keys and SSH targets.
Workflow inputs enter shell steps only through environment variables.

`scripts/test_release.py` uses Python's standard library, builds the real Go signer
in a temporary directory, creates a disposable key, and exercises:

- Go-generated raw Ed25519 signatures verified by OpenSSL, tampered checksums,
  invalid signatures, mismatched keys and rejected versions.
- Actual linker-injected public key and normalized version, verification of the
  Go signer's manifest by the client, and rejection of a changed manifest.
- Actual Linux installer with a fixture-only curl executable, both accepted version
  forms, public/GitHub naming, changed binaries, invalid signatures and old hex
  signature encoding. Its transport mock is checked before execution, so a missing
  mock fails closed rather than downloading anything.
- Windows installer signature and checksum functions and file replacement under
  separate Windows PowerShell 5.1 and PowerShell 7 processes. The helper asserts
  the runtime major version. It verifies real signatures, rejects tampered files
  and duplicate checksums, preserves the legacy install location, removes the
  competing bin copy and restores the helper after a partial installation failure.
  User PATH/registry changes and actual installed binaries are not touched.
- Strict tag/commit/migration/SSH/key rejection and all workflow-local source paths.
  The Go linker test is explicitly listed to prevent a nonexistent test name from
  yielding a misleading success with zero executed tests.

`deploy/hetzner/test_deploy.py` runs only in a **disposable Linux root sandbox**.
It rewrites a private copy of the real wrapper's paths and service commands, checks
that every rewrite matched, signs actual fixture bundles with Go, and leaves real
OpenSSL verification intact. It covers legacy executable preservation, deployment
retry, publication identity, rollback, changed migration plans, invalid signatures,
missing artifacts, invalid inputs, receipt ownership/ancestry/symlinks/release
binding, failed migrations, SIGKILL interruption, health failure, rollback health
failure, rollback activation failure, and dry-run. A separate real systemd test
checks that a root-only dummy EnvironmentFile is readable by systemd before it
drops privileges, while the unprivileged user cannot read the file directly.
Neither test uses a real DB URL, production SSH connection or production receipt.

## Commands

From the repository root on Windows (Go, Python 3.10+, Git for Windows/OpenSSL 3,
Windows PowerShell 5.1 and PowerShell 7 installed):

```powershell
python scripts/test_release.py
go test ./... -skip TestPostgreSQLIntegration -count=1
go vet ./...
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 -shellcheck= -pyflakes=
git diff --check
gofmt -l .
```

On the Ubuntu CI runner, use the isolated CI PostgreSQL service and run:

```sh
python3 scripts/test_release.py
go test -race -count=1 ./...
go build -o /tmp/sockt-test-signer ./cmd/sockt-release
sudo bash deploy/hetzner/test-sockt-deploy.sh /tmp/sockt-test-signer
for script in scripts/*.sh deploy/hetzner/sockt-deploy deploy/hetzner/test-sockt-deploy.sh; do
  bash -n "$script"
done
```

The CI workflow runs on pull requests, master and feat/release-automation pushes,
and manual CI dispatch. It keeps both Linux and Windows jobs. The Windows Python
suite invokes both PowerShell executables; a missing or wrong runtime fails the
job. Linux intentionally skips the Windows-only method, which is exercised in the
Windows job. ShellCheck/Pyflakes are not installed locally; actionlint's own YAML,
expression, context and workflow checks still run. Bash syntax checks remain enabled.

## Local results and outstanding runner checks

Executed on this Windows host: release regression suite (including real OpenSSL
verification and both PowerShell runtimes), Go tests excluding external PostgreSQL,
Go vet, actionlint, Python syntax, Bash syntax, formatting and whitespace checks.
PowerShell versions were 5.1.26100.8972 / CLR 4.0.30319.42000 and 7.6.5 / .NET
10.0.11, with OpenSSL 3.5.7. Linux installer execution here used Git Bash; this is
not a claim of native Linux OS behavior or end-to-end HTTPS testing.

Native Linux deployment/systemd/ownership tests, CI PostgreSQL integration and race
checks require the Ubuntu runner. The local Docker daemon is unavailable, and the
local Go installation has CGO disabled with no C compiler. Hosted jobs cannot be
reported green before these changes are committed, pushed and run. The tests do
not validate production permissions, proxy routing, backup quality, secret setup
or installed public-key provenance. Those remain explicit first-release gates in
`RELEASING.md` and `deploy/hetzner/RELEASE_AUTOMATION.md`.
