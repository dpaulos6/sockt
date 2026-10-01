# Restricted release deployment setup

Do these steps manually on Hetzner after review. They never install the signing
private key on the VPS.

1. Create an unprivileged `socktdeploy` account and an SSH key dedicated to
   GitHub Actions. It owns only `/var/lib/sockt-deploy/incoming`.
2. Install `sockt-deploy` root-owned mode `0750` at `/usr/local/sbin/` and use
   a narrow sudoers rule permitting only its deploy, publish, status and
   rollback subcommands. Do not allow root SSH login or a general shell.
3. Verify the VPS host key out of band; store its complete known-hosts line as
   `HETZNER_SSH_KNOWN_HOSTS`.
4. Store the existing public Ed25519 key as root-owned
   `/etc/sockt/release-public-key.pem`; it verifies signed release checksums.
5. For migrations, verify an off-VPS Neon backup first and create a root-owned
   receipt `/var/lib/sockt/backups/<backup-id>.verified`.

The wrapper rejects concurrent deployments, malformed values, failed checksums,
missing backups, and health-check failures. It never rolls back a database
migration.
