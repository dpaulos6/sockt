# Restricted release deployment setup

These are prerequisites for a separately approved deployment, not actions taken
by the CI repair. Never put the private signing key or production credentials into
the checkout or tests. See `docs/RELEASING.md` for baseline/tag preparation.

## Ownership and commands

Provision these real directories root-owned, with no group/other write access in
any ancestor and no symlinks. The wrapper checks the full ancestry:

- `/opt/sockt` and `/opt/sockt/releases`: root:root 0755. The existing regular
  `/opt/sockt/socktd` must also be root-owned and executable. Verify its provenance
  and checksum before first deployment; do not merely adopt an untrusted binary.
- `/var/lib/sockt-deploy`: root:root 0755.
- `/var/lib/sockt-deploy/state` and `/var/lib/sockt-deploy/backups`: root:root 0700.
- `/var/www/sockt-updates`: root:root 0755, served read-only by the web server.
- Only `/var/lib/sockt-deploy/incoming` is writable by `socktdeploy`. This account
  must not be able to replace the parent, state, backups, key or deployment script.

Install the wrapper root-owned 0750 at `/usr/local/sbin/sockt-deploy`, with a narrow
sudoers rule for deploy, publish, rollback and status. Do not permit `SETENV`, root
SSH login, arbitrary sudo commands, or writes to the wrapper. SSH upload commands
need the restricted account's normal unprivileged shell; any forced-command SSH
configuration must explicitly accommodate those upload operations.

The wrapper uses a fixed PATH, `/usr/sbin/runuser`, `/usr/bin/pm2` and the existing
`paulos` PM2 account. Confirm the actual PM2 binary path/service identity before
installation; change the reviewed root-owned wrapper if the host differs. Never
resolve privileged executables from the deploy account's PATH. Install OpenSSL 3,
GNU coreutils, util-linux, curl, systemd and the verified existing public key as
`/etc/sockt/release-public-key.pem`. Verify the SSH host key out of band.

## Migration credentials and backup receipt

Create a dedicated unprivileged `socktmigrate` OS user/group. Store the migration
role's database configuration in root:root 0600
`/etc/sockt/socktd-migrate.env`, with a protected `/etc/sockt` parent. Use systemd
EnvironmentFile syntax (`DATABASE_URL=...`), not an executable shell script. The DB
role should have only the schema privileges required by reviewed migrations;
never give the SSH deployment account database credentials or broad superuser
access. Existing runtime database configuration is unchanged.

`systemd-run` reads this root-only EnvironmentFile **before** dropping to
`socktmigrate`, with NoNewPrivileges, ProtectSystem, ProtectHome, PrivateTmp and a
bounded runtime. The URL is never placed in command arguments. Migration output
is suppressed from CI/terminal output to prevent driver errors leaking credentials.
A failed migration leaves a journal and the old executable active; inspect schema
state with a separately authorized administrator before retrying or rolling back.

After verifying an off-host backup/restore, a root administrator creates
`/var/lib/sockt-deploy/backups/<receipt-id>.verified`, mode 0600, regular file,
single hard link, containing exactly three newline-separated lines:

```text
v0.10.0
<full 40-character release commit>
<64-character SHA-256 of signed socktd-linux-amd64>
```

Receipts expire after 24 hours, cannot be symlinks, and are bound to the release.
The wrapper checks root ownership and nonwritable ancestry, so ownership of a leaf
file alone is insufficient. `socktdeploy` must be unable to create, touch or replace
receipts. Only the administrator attests to a verified backup; CI cannot self-attest.

## First deployment, failure and recovery

The first deployment copies the existing regular executable to a content-addressed
`releases/socktd-legacy-<sha256>`, checks that copy, and records its digest before
replacing `/opt/sockt/socktd` with a symlink. Later deployments retain the previous
versioned target. Rollback validates the recorded digest and root-owned location.
The recovery journal is persisted before migration or activation, and a pending
journal blocks further deployment/publication until resolved. Failed restart or
health checks attempt executable rollback. A failed rollback retains the journal
and returns failure; it never reports success after a failed symlink replacement.

Dry-run verifies the signed snapshot, inputs and prerequisites without running the
uploaded executable, migrations, restart or activation. Temporary verification
files and the deployment lock are still used. Retries of a healthy completed
release preserve its original rollback record. Database migrations are never
rolled back automatically; all approved schema changes must support the old binary.

Publication verifies the exact deployed identity, writes immutable public installer
bundles and client updater files, then switches `stable.json` last. Public
`/updates/releases/` routing must serve the directory tree, not only flat files.
Interrupted publication may leave an unused hidden staging directory; an
administrator can inspect/remove it later without altering a published version.
