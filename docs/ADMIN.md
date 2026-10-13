# Account-bound administration

Admin access belongs to one immutable PostgreSQL `users.account_uuid`, derived
by the server from the authenticated session. Usernames, emails, client configuration,
and client-supplied capabilities cannot grant access. No admin credential or
signing key is added to the client. An unset or empty `SOCKT_ADMIN_USER_ID`
disables all admin operations.

## Find your account UUID

In an operator-controlled PostgreSQL console connected to Sockt's database,
run this read-only query, substituting your registered username:

```sql
SELECT account_uuid, username FROM users WHERE username_key = lower('YourUsername');
```

This selects only account UUID and username. Do not select password hashes,
sessions, invite hashes, recovery codes, or database connection settings. Check
that the returned account is yours. The username is used only for this local
lookup; authorization subsequently compares the immutable UUID. If the account
is deleted and recreated, obtain its new UUID rather than reusing the old one.

## Configure the server

Set `SOCKT_ADMIN_USER_ID=8f6cc9b8-15e2-4ed8-b3da-f615e419ed9a` in the
**server process environment**, replacing the example UUID with your own lookup
result. Socktd does not load `.env` itself: use your existing operator-managed
service environment. Do not add this setting to client config.

Restart `socktd` with the new environment. Existing clients reconnect and receive
the updated capability. Restart is also required to change or remove the admin
account. Migration `003_account_uuid.up.sql` is required before starting this
server. Only canonical lowercase, hyphenated, non-nil UUIDs are accepted. Integer
IDs, uppercase, whitespace, malformed UUIDs and the nil UUID cause serve startup
to fail before connecting to PostgreSQL. The UUID is not a secret; knowledge of
it does not authenticate a user. There is no fallback to the legacy integer ID.

## Use the panel

On the authorized account, `/admin` appears in command completion and F1 help.
It opens a compact panel on the existing chat connection. Chat history continues
to arrive; returning to chat preserves its draft and scroll position.

- `R`: refresh server version, commit, process uptime, authenticated chat connection
  count (multiple devices count separately), and account count.
- `C`: enter a username and confirm invite creation with Enter/Y. The existing
  one-time, username-bound invite mechanism generates the secret and stores only
  its hash. Save the displayed invite securely and give it only to the intended
  person; `A` acknowledges and clears it. Leaving the panel also clears it.
- `V`: enter a username and confirm revocation of **all unused invites** for that
  username. Used invitations and registered accounts are unaffected. Repeating
  revocation is harmless and reports zero when none remain.
- `N`: cancel confirmation. `Esc` cancels entry, or returns from the overview to
  chat. `PgUp/PgDn` scroll details on a small terminal.

While a request is in flight, navigation waits for its result. Mutations are
never automatically retried after a connection loss; if the result is lost, an
invite may already have been created. Revoke unused invites for the username
before creating another if necessary. Status is a snapshot; `R` refreshes it.
Development builds may report `dev` / `unknown` until build metadata is injected.

Revocation deletes unused invite rows. It uses PostgreSQL row locking compatible
with registration's `SELECT ... FOR UPDATE`: whichever operation gets the row
first determines whether registration succeeds. Consumed invite rows remain;
revocation cannot undo completed registration. No schema extension is needed.

## Authorization and audit

Every admin operation revalidates the original authenticated session and checks
its database-authenticated account UUID centrally on the server. Capability visibility is only a UI hint.
Unauthorized protocol requests cannot read status or mutate invites. Logout,
password/session replacement, connection loss, and failed authentication clear
the client's capability and displayed invite; reconnect must obtain it again.

Each invite mutation attempt records `admin_audit` in the existing server logger:
actor account UUID (`actor_id`), action, validated target username, UTC timestamp, and outcome
(success, failure, denial, invalid request, or unavailable service). Invalid
targets are represented as `invalid`. Database error details and invite secrets
are excluded. Retain the server's logs using your existing operator log policy;
this is process logging, not a transactional database audit table.

The panel has no shell execution, SQL console, deployment controls, or
impersonation. It does not expose passwords, sessions, or recovery data.

## Verification

Run `go test -race ./...`, `go vet ./...`, and `go build ./...`. Database-backed
invite revocation tests use `SOCKT_TEST_DATABASE_URL` and skip when it is unset.
Point that setting **only at a disposable test database**: the existing database
suite migrates and writes test data. Do not use production credentials or data.

## Stage 1 migration and release gate

This release adds only `users.account_uuid UUID`: database-generated UUIDv4,
backfill of existing accounts, a database default for old-binary registrations,
and UNIQUE/NOT NULL constraints. Migration `003_account_uuid.up.sql` runs in the
existing migration transaction with its ledger entry. Failed migrations roll back
both; rerunning successful migrations retains the original mappings. `users.id`,
its sequence, and all integer account foreign keys remain unchanged. No child
UUID columns, triggers, contraction, account recreation, or credential rotation
are part of this stage. Existing client sessions and message cursors remain valid;
there is no new client configuration or protocol version.

Before deploying this release:

1. Confirm the exact previous production executable's version and commit and retain
   its checksum-verified rollback copy. Run the compatibility test with that
   unchanged executable against a disposable database; a tag alone does not prove
   the deployed executable's provenance.
2. Verify an off-host backup/recovery point and restore it into an isolated database.
   Include accounts, messages, memberships, sessions, recovery codes, invites,
   migration ledger and identity sequences. Preserve the `(id, account_uuid)` mapping
   after migration in subsequent backups. Do not log database URLs or credentials.
3. A root operator creates `/var/lib/sockt-deploy/backups/<receipt-id>.verified`,
   mode 0600, a regular file with one hard link and protected ancestry. Its exact
   three newline-terminated lines are the intended release tag, full 40-character
   release commit, and 64-character SHA-256 of the signed server artifact. The
   receipt must be less than 24 hours old and attest to verified backup restoration.
   See [deployment automation](../deploy/hetzner/RELEASE_AUTOMATION.md).
4. The signed release must declare `migration-plan` as `required`. Deployment needs
   explicit `--allow-migrations --backup-id <receipt-id>` in addition to the wrapper's
   normal version/commit/checksum arguments. The protected migration environment is
   `/etc/sockt/socktd-migrate.env`; never pass a database URL in command arguments.
5. Apply the reviewed migration **before** activating the UUID-aware server. The
   wrapper follows this order while the previous binary is still running. Measure
   table-lock/backfill duration on a restored copy: `ALTER TABLE`/backfill can block
   writes, and the wrapper bounds migration runtime to 300 seconds. Schedule a
   maintenance window if needed. Standalone operators use `socktd migrate` with a
   protected process environment; serve never applies migrations automatically.
6. After migration, perform the UUID lookup above, set `SOCKT_ADMIN_USER_ID` to that
   UUID (or leave it empty), and restart with the updated server environment. Check
   authenticated login, chat writes, account operations, and both admin access and
   denial—not only `/healthz`. The new server requires migration 003 at startup.

Executable rollback leaves the additive schema and UUID mappings intact. Tagged
v0.10.0 and v0.9.2 binaries retain integer queries and ignore the admin setting;
admin features disappear during rollback. Their registrations still receive UUIDs
from the database default. **Do not run down migrations or remove the UUID column
for executable rollback.** The unreleased integer-admin implementation is not a
supported rollback target when the environment contains a UUID. Any future
contraction requires a separate compatibility review. If database restoration is
necessary, restore database, binary and environment as a consistent set and assess
writes made since the recovery point; executable rollback is not data recovery.

To reproduce database-backed validation, set `SOCKT_TEST_DATABASE_URL` only to a
fresh disposable database. For the previous-binary test, also set
`SOCKT_TEST_PREVIOUS_SERVER` to the absolute path of the unchanged executable and
`SOCKT_TEST_PREVIOUS_VERSION` to its exact `socktd version` output. For older binaries without a version command, use
`SOCKT_TEST_PREVIOUS_SHA256` instead, containing the verified executable digest.
Do not substitute a guessed version. Run:

```bash
go test -race -count=1 -v ./internal/database ./internal/server
go test -race -count=1 ./...
go vet ./...
go build ./...
```

The migration tests use isolated temporary schemas and exercise full data
preservation, constraint failure/atomic rollback, stable UUID mappings and old
registration defaults. The previous-binary test runs its actual migration,
registration, password login/session creation and WebSocket chat before and after
migration, then tests UUID admin authorization using real database sessions. It
skips if any required setting is absent; a skip is not integration validation.
