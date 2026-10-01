# Disaster recovery

Keep verified Neon backups and regularly test restoration into an isolated
database. Preserve the previous `/opt/sockt/socktd` executable during each
deployment. Server binary rollback is safe only when the deployed schema is
compatible; never automatically roll back database migrations. Preserve the
Ed25519 private signing key offline and outside Git, GitHub logs, and Hetzner.
