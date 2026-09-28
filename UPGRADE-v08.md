# Upgrading Sockt v0.8 to v0.9

**Read [`docs/ACCOUNTS-v09.md`](docs/ACCOUNTS-v09.md) before deploying.**

v0.9 adds invitation-only recovery codes, a terminal account-management menu,
multiple concurrent device sessions, device revocation, password change and
one-time password recovery. An additive `002_accounts.up.sql` Neon PostgreSQL
migration preserves your v0.8 accounts and conversations; deploy the server
and migration before distributing the new client. Reuse your existing v0.8
Ed25519 release-signing key. The server stays under PM2 on Hetzner.
