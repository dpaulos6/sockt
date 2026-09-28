# Sockt v0.7 on Hetzner — PostgreSQL + WSS

**Target:** Ubuntu 24.04 x86_64, Docker Compose, your existing HTTPS reverse proxy. No Tailscale required. `socktd` accepts **HTTP/WebSocket on 127.0.0.1:8080 only**. Your proxy provides public TLS at `wss://chat.yourdomain.com/ws`. Existing Next.js `:3000` and its current domain configuration are unaffected when you create a separate subdomain/vhost.

## A. Prepare DNS and verify your current proxy

1. Make DNS A/AAAA for `chat.yourdomain.com` point to your Hetzner VPS. Remove an incorrect AAAA record if you do not have IPv6 configured. Keep your current Next.js subdomain untouched.
2. Inspect the current Nginx/Caddy/Traefik configuration and existing certificate automation **before editing anything**. Follow only the matching proxy example from `../proxy/`; the samples are not a blind drop-in for every existing VPS. You need a valid trusted HTTPS certificate for **the exact chat hostname**.
3. Preserve SSH access and don't change existing Next.js ports. Your firewall should allow inbound HTTPS 443 to the existing proxy. Do **not** open public TCP 8080 or 5432.

## B. Database

```bash
sudo mkdir -p /opt/sockt/db
# Copy deploy/hetzner/compose.yaml into /opt/sockt/db/compose.yaml
cd /opt/sockt/db
umask 077
# Create .env privately: POSTGRES_PASSWORD=<generate a long unique secret>
docker compose up -d
# Verify: docker compose ps
```

The Docker Compose config publishes PostgreSQL **only** to `127.0.0.1:5432` on Hetzner. Ensure you don't already run something else on that port before starting it. Never place the real DB password inside a Git repository. Docker volume `sockt_db` is persistent, but it is **not** a backup.

## C. Upload and activate Sockt

Build the server binary with `bash scripts/build.sh` locally or directly on your VPS after installing a compatible Go toolchain. Upload `dist/socktd-linux-amd64` to `/opt/sockt/socktd`, then:

```bash
sudo chmod 755 /opt/sockt/socktd
id sockt || sudo useradd --system --home /var/lib/sockt --shell /usr/sbin/nologin sockt
sudo install -d -m 0750 -o sockt -g sockt /var/lib/sockt
sudo install -d -m 0700 -o root -g root /etc/sockt
# Copy deploy/systemd/socktd.env.example to /etc/sockt/socktd.env
sudo chown root:root /etc/sockt/socktd.env
sudo chmod 600 /etc/sockt/socktd.env
# Edit DATABASE_URL with URL-encoded special chars in password.
```

Set `SOCKT_LISTEN=127.0.0.1:8080`. On upgrades from v0.6 **reuse the existing DB volume**, do not re-run JSON import if you already imported it, and retain users/sessions/messages. Run `socktd migrate` using the real DATABASE_URL via a protected admin shell; current v0.7 adds **no schema migration**. Create registration invites with `socktd invite <username>` when you need a new account. Never paste invite tokens into logs/chats visible to others.

```bash
# Copy deploy/systemd/socktd.service -> /etc/systemd/system/socktd.service
sudo systemctl daemon-reload
sudo systemctl enable --now socktd
sudo systemctl status socktd --no-pager
curl --fail http://127.0.0.1:8080/healthz
```

Note: `sudo` doesn't automatically load `DATABASE_URL` from `/etc/sockt/socktd.env`. Load it deliberately for administrative commands, **without printing it or adding secrets to shell history**. Keep root-only env file permissions.

## D. Reverse proxy, TLS, validation

- **Caddy:** merge `deploy/proxy/Caddyfile.example` into your existing config for the new subdomain, validate and reload Caddy. Caddy obtains TLS once DNS is correct.
- **Nginx:** use `deploy/proxy/nginx-chat.example.conf` as a separate TLS vhost; obtain a trusted certificate for the chat hostname with your normal Certbot or existing certificate management flow. `sudo nginx -t` before reloading. WebSocket upgrades must pass `Upgrade` and `Connection` headers. Never blindly overwrite your working Next.js config.
- Another proxy (Traefik, cloud panel): route only exact `/ws` on chat hostname to `http://127.0.0.1:8080`, enable WS upgrade and HTTPS. Your existing Next.js route should remain as it is.
- Test `curl -I https://chat.yourdomain.com/` (expected 404) and `wss://chat.yourdomain.com/ws` using the client. A normal curl GET to `/ws` is **not** a WebSocket test; a successful WS handshake returns HTTP 101.

## E. Install client / invite friends

Distribute `sockt-windows-amd64.exe` from the build script. On the first run, enter `wss://chat.yourdomain.com/ws`, their username, invitation code, and their **own** password. Future runs use saved session credentials and automatic reconnect. Friends do not need Tailscale, Go or the repo.

## F. Verify before you share

- Run `go mod tidy && go test -race ./...` locally (or GitHub CI), then build both binaries. The network integration test in `internal/server/ws_integration_test.go` must pass with **the actual coder/websocket dependency**.
- Check successful signup/login, invalid credentials, concurrent broadcast, re-login, reconnect and history after service restart.
- Check `ss -lntup` shows Sockt and PostgreSQL bound only to `127.0.0.1`. Check your public firewall doesn't expose `:8080` or `:5432`.
- Back up PostgreSQL regularly to a separate protected location. Example from `/opt/sockt/db`: `docker compose exec -T postgres pg_dump -U sockt_app -d sockt -Fc > sockt-$(date +%F).dump`. Verify restores in a **separate disposable database**.

## Security limits

WSS protects network transport, but **Sockt is not end-to-end encrypted**: server admins can access PostgreSQL message content. The v0.7 CLI session token lives in an OS-user-only config file (not system keychain); never use it on a shared computer. This is a small invite-only beta; no password recovery, session revocation UI, independent security audit or abuse-resistant public signup. Do not claim otherwise.
