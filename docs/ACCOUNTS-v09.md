# Sockt v0.9 — Invitation-only accounts, recovery and sessions

**Deployment:** v0.9 server first, v0.9 clients afterward. This is an additive
PostgreSQL migration over the existing Neon v0.8 schema and retains your
accounts, chat history, existing password hashes and pre-existing sessions.
No mandatory email, SMTP, OAuth, or Tailscale. Your HTTPS/WSS proxy is unchanged.

## New account flows

- `sockt setup`: select Register, Login or Forgot password. New users need the
  server-generated invitation for their username. They choose a password and a
  human-readable device label. Registration generates **eight recovery codes**.
- Recovery codes appear **only once** during registration or on manual rotation.
  Each is 160 random bits, printed as Base32 groups and stored only as SHA-256
  hashes server-side. Write them in a private password manager. They are NOT
  automatically saved to disk or sent by email.
- `sockt recover`: username, one unused recovery code, and a new password. The
  used code and **all remaining codes in that set** become invalid. All old
  sessions are revoked, a new current session is created and eight fresh
  recovery codes appear once. Store the replacement set securely.
- `sockt login`: username/password and optional device label; issues an
  independent 30-day session for that device. Existing v0.8 sessions remain
  valid unless revoked/expired.
- `sockt account` or `/account` / `Ctrl+P` inside chat opens the account menu:
  see active sessions, revoke a specific session, change password, rotate all
  recovery codes (requires password), or log out the current device.
- `sockt logout` revokes only the local device's session and removes its locally
  cached token. Changing/resetting a password revokes ALL other sessions.
- Same username can connect from multiple computers concurrently. Presence
  still counts unique users, not connected devices.

Current session tokens remain in user-only local `config.json`. OS credential
vault storage, self-service email recovery, MFA, and friend lists are later
projects. The nullable `users.email` field is reserved for optional verified
email support; **do not store unverified addresses or pretend email recovery is
available**.

## Deploy on your Hetzner VPS with PM2

**Never run `go test` with `SOCKT_TEST_DATABASE_URL` set to your live Neon DB.**
The database integration tests assume a fresh disposable database and insert
sample accounts. First test v0.9 in a newly created *disposable Neon branch*
or independent local PostgreSQL database.

1. On your development PC, apply the upgrade after v0.8 and run `go mod tidy`,
   `go vet ./...`, `go test -race ./...`, and `go build ./...`. On a fresh
   disposable DB, additionally set `SOCKT_TEST_DATABASE_URL` and run the
   integration test. **Verify v0.8 updater actually works on your platform**
   before attempting in-app delivery of v0.9.
2. Create a Neon recovery point / backup. From Hetzner, optionally produce an
   additional encrypted, off-server backup using `pg_dump`. If the private
   backup stays on the VPS temporarily:

   ```bash
   sudo install -d -m 0700 /var/backups/sockt
   sudo bash -c '
   umask 077
   set -a
   . /etc/sockt/socktd.env
   set +a
   pg_dump -Fc "$DATABASE_URL" > "/var/backups/sockt/sockt-pre-v09-$(date -u +%Y%m%dT%H%M%SZ).dump"
   '
   ```

   Copy the backup **off the VPS** and test restoring into an isolated database.
   The SQL migration is additive; don't drop recovery codes to roll back.

3. Merge/review and push v0.9 to your private `dpaulos6/sockt` repo. On Hetzner:

   ```bash
   cd /opt/sockt/src
   git pull --ff-only
   go mod download
   go vet ./...
   go test ./...  # production env must NOT define SOCKT_TEST_DATABASE_URL
   go build -o /opt/sockt/socktd.v09.new ./cmd/socktd
   ```

4. **Migrate while v0.8 is still running.** The new columns are additive and
   have defaults compatible with the old binary.

   ```bash
   sudo bash -c '
   set -a
   . /etc/sockt/socktd.env
   set +a
   exec /opt/sockt/socktd.v09.new migrate
   '
   ```

   Ensure `schema_migrations` contains `002_accounts.up.sql` afterward.

5. Switch the binary atomically and restart **PM2** (not the now-disabled
   systemd `socktd` service):

   ```bash
   cp /opt/sockt/socktd /opt/sockt/socktd.v08.backup
   mv /opt/sockt/socktd.v09.new /opt/sockt/socktd
   pm2 restart socktd
   pm2 logs socktd --lines 30
   curl -fsS http://127.0.0.1:8080/healthz
   ```

   The v0.8 binary can be restored if necessary. Avoid attempting a schema
   rollback: v0.8 operates with the additive v0.9 schema.

6. On your **existing account**, run the v0.9 client `sockt account`, select
   **Generate new recovery codes**, re-enter your password and store the codes
   securely. No new invitation is necessary. Test logging in on a second device
   under the same username. Test session revocation, password change and recovery
   on a disposable account before encouraging friends to use it.

## Publish the signed v0.9 client release

Keep your **original v0.8 Ed25519 private key on your Windows development PC**.
Use the same public key and signing key that existing updater-enabled clients
trust. Do not generate a new signing key, and never upload it to Hetzner.

```powershell
$env:SOCKT_UPDATE_PUBLIC_KEY = 'THE SAME PUBLIC KEY AS v0.8'
$env:SOCKT_RELEASE_VERSION = '0.9.0'
.\scripts\build.ps1

go run ./cmd/sockt-release sign `
  -key "$env:USERPROFILE\.config\sockt\release-key.hex" `
  -version 0.9.0 `
  -base-url https://chat.dpaulos.pt/updates `
  -dist dist `
  -out dist/stable.json
```

Verify the locally built binaries report `0.9.0` and confirm the SHA-256 values
match the manifest. Upload all **versioned binaries first**, verify each is
accessible over HTTPS, and replace `/var/www/sockt-updates/stable.json` **last**.
Only publish after the v0.9 server is healthy and account recovery/session tests
pass. The existing Nginx `/ws` and `/updates` paths require no changes.

## Security and limitations

- Invite-only is not end-to-end encryption. The server/Neon administrator can
  still access message contents.
- Recovery secrets must never appear in application logs, crash reports,
  public screenshots, chat messages, Git commits, or shared terminal recordings.
- Password operations are rate-limited per username in process, with a bounded
  worker pool. This is a small private beta, not a large public signup service.
  Multi-instance deployments would need shared rate limits and revocation events.
- Existing active connections are closed immediately on the same server when
  their sessions are revoked; chat requests and heartbeats also revalidate
  credentials against PostgreSQL. In a future multi-server deployment, use
  distributed session-revocation notifications.
- Without an existing password **or** an unused recovery code, there is no
  self-service recovery; manual admin recovery is deliberately not included.
- Account recovery hashes and session labels are persisted in Neon. Back up the
  database and test point-in-time restoration independently.
