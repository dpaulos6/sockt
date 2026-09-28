# Sockt v0.8: signed in-app client updates

**Client-only.** The VPS/server still updates through your normal Git/systemd/PM2
procedure. The updater never runs on Hetzner automatically and never changes
PostgreSQL. v0.7 clients require one **manual** installation of v0.8 before
they can update themselves.

## User experience

An installed v0.8 client with a trusted release public key checks
`https://<configured-WSS-host>/updates/stable.json` at startup and every 30
minutes while open. A signed, newer release matching the client's OS/arch
appears in the terminal header/footer. **F2**, then **Y**, downloads it and
checks SHA-256 against the signed manifest; the app exits the TUI and replaces
itself, then restarts. **N** cancels. No updates install without consent.

On Linux it replaces and `exec`s in the same foreground terminal. Windows
requires the accompanying `sockt-updater.exe` alongside `sockt.exe`: the helper
waits for the previous executable to exit, installs the staged binary, and
restarts it in the same PowerShell or Windows Terminal session. If the app is
installed somewhere the current user cannot modify, the download fails safely
and the old binary stays intact; use a user-writable directory or conventional
package manager.

The updater does not download from a private GitHub repository or require
users' GitHub credentials. Host *public binary artifacts only* on the VPS.

## One-time release signing setup (developer PC, **not** Hetzner)

1. Generate an Ed25519 key pair exactly once. Keep the private seed **out of
   Git, your VPS, backups with broad access, and all user-visible logs**:

   ```powershell
   mkdir "$env:USERPROFILE\.config\sockt" -Force
   go run ./cmd/sockt-release keygen -out "$env:USERPROFILE\.config\sockt\release-key.hex"
   ```

   Save the printed **PUBLIC** hex key. Never regenerate it casually: clients
   built with the old public key will reject future manifests signed by another
   private key.

2. Build the first updater-enabled version with that public key embedded:

   ```powershell
   $env:SOCKT_UPDATE_PUBLIC_KEY = 'PASTE_PUBLIC_KEY_HEX'
   $env:SOCKT_RELEASE_VERSION = '0.8.0'
   .\scripts\build.ps1
   ```

   On Windows, **distribute BOTH** `sockt-windows-amd64.exe` (rename to
   `sockt.exe`) and `sockt-updater-windows-amd64.exe` (rename to
   `sockt-updater.exe`) together in a user-writable folder. The existing Sockt
   local account config is untouched. Linux users get a single binary.

3. Publish the first release even if nobody will receive an update yet:

   ```powershell
   go run ./cmd/sockt-release sign `
     -key "$env:USERPROFILE\.config\sockt\release-key.hex" `
     -version 0.8.0 `
     -base-url https://chat.dpaulos.pt/updates `
     -dist dist `
     -out dist/stable.json
   ```

   **Don't** commit the signing key or release manifest to the private source
   repository. The signed manifest is a *public deployment artifact*.

## Nginx: static artifact route

Create the public directory on Hetzner; the Nginx worker must be able to read it:

```bash
sudo install -d -m 0755 -o root -g root /var/www/sockt-updates
```

Add this **inside the existing HTTPS server block for chat.dpaulos.pt**, next
to `location = /ws` and before the existing fallback `location /`:

```nginx
location ^~ /updates/ {
    alias /var/www/sockt-updates/;
    autoindex off;
    add_header Cache-Control "no-cache" always;
    limit_except GET { deny all; }
}
```

Keep your current TLS, Certbot and `/ws` configuration. Validate and reload:

```bash
sudo nginx -t && sudo systemctl reload nginx
```

Do **not** place these downloads in `/opt/sockt/src`, nor the signing key on the
VPS. Static updates work while the chat server is down, provided Nginx is up.

## Every subsequent client release

1. Update source and tests. Bump the release version using
   `SOCKT_RELEASE_VERSION`. **Never ship a manifest for version X containing a
   binary compiled as version Y** (the running client will compare its version).
2. Build and test on a disposable environment first. Keep Windows and Linux
   compatibility. Windows helper remains beside the current client; if it ever
   needs a breaking upgrade, distribute it separately with care.
3. Sign with the **same private key** as the initial release:

   ```powershell
   $env:SOCKT_RELEASE_VERSION = '0.8.1'
   .\scripts\build.ps1
   go run ./cmd/sockt-release sign `
     -key "$env:USERPROFILE\.config\sockt\release-key.hex" `
     -version 0.8.1 `
     -base-url https://chat.dpaulos.pt/updates `
     -dist dist `
     -out dist/stable.json
   ```

4. Upload versioned binaries to the VPS **first**:

   ```powershell
   ssh paulos@YOUR_VPS 'mkdir -p /tmp/sockt-release'
   scp dist/sockt-windows-amd64.exe paulos@YOUR_VPS:/tmp/sockt-release/sockt-v0.8.1-sockt-windows-amd64.exe
   scp dist/sockt-linux-amd64 paulos@YOUR_VPS:/tmp/sockt-release/sockt-v0.8.1-sockt-linux-amd64
   scp dist/sockt-linux-arm64 paulos@YOUR_VPS:/tmp/sockt-release/sockt-v0.8.1-sockt-linux-arm64
   scp dist/stable.json paulos@YOUR_VPS:/tmp/sockt-release/stable.json
   ```

   Check the filenames match the signed manifest. On Hetzner:

   ```bash
   sudo install -m 0644 /tmp/sockt-release/sockt-v0.8.1-* /var/www/sockt-updates/
   curl -I https://chat.dpaulos.pt/updates/sockt-v0.8.1-sockt-windows-amd64.exe
   sudo install -m 0644 /tmp/sockt-release/stable.json /var/www/sockt-updates/stable.json.next
   sudo mv /var/www/sockt-updates/stable.json.next /var/www/sockt-updates/stable.json
   curl -fsS https://chat.dpaulos.pt/updates/stable.json
   ```

   Replacing the manifest **last** prevents clients from seeing a release
   before all of its binaries are available. Use `sudo chown root:root` if
   artifacts came from another source.

## Security and deployment boundaries

- Client only accepts a release manifest signed by the embedded Ed25519 key.
- Both manifest and binary use HTTPS on the user's configured WSS hostname.
- Downloads have bounded sizes and SHA-256 verification. Bad signatures or
  hashes never replace the installed executable.
- Downloads are staged beside the current binary for same-filesystem rename;
  failed installation attempts preserve the original binary wherever possible.
- If you lose the private signing key, old clients **cannot** trust a replacement
  key automatically. Securely archive it offline.
- Publishing to `/updates` is **manual** for now. Automating it via a properly
  permissioned CI release pipeline can come later; never put the private key
  directly in the repository.
