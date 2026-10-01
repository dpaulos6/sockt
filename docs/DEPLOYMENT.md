# Deployment

Production is Ubuntu 24.04 on Hetzner. `socktd` runs as user `paulos` under
PM2 from `/opt/sockt/socktd`; its protected environment is
`/etc/sockt/socktd.env`. Nginx terminates TLS and proxies `/ws`; health checks
use `http://127.0.0.1:8080/healthz`. Neon hosts PostgreSQL.

No release workflow in this phase connects to Hetzner. The deployment phase
will use a restricted deployment identity, checksum-verified artifacts, PM2
health checks, executable rollback, and an explicit migration approval gate.
