#!/usr/bin/env bash
set -euo pipefail
version="${1:-__SOCKT_VERSION__}"
repo="${SOCKT_REPOSITORY:-dpaulos6/sockt}"
public_key="__SOCKT_PUBLIC_KEY__"
bin_dir="${XDG_BIN_HOME:-$HOME/.local/bin}"
if [[ "$version" == __SOCKT_* || "$public_key" == __SOCKT_* ]]; then echo 'unrendered installer template' >&2; exit 2; fi
case "${SOCKT_INSTALL_ACTION:-install}" in
  uninstall) rm -f "$bin_dir/sockt"; echo 'Sockt binary removed; configuration preserved.'; exit 0;;
  install|update|reinstall) ;;
  *) echo 'invalid SOCKT_INSTALL_ACTION' >&2; exit 2;;
esac
command -v curl >/dev/null && command -v openssl >/dev/null && command -v xxd >/dev/null || { echo 'curl, openssl and xxd are required' >&2; exit 1; }
base="https://github.com/$repo/releases/download/v$version"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
curl --fail --location --proto '=https' --tlsv1.2 "$base/sockt-v$version-checksums.txt" -o "$work/checksums"
curl --fail --location --proto '=https' --tlsv1.2 "$base/sockt-v$version-checksums.txt.sig" -o "$work/checksums.sig"
printf '%s' "$public_key" | xxd -r -p | { printf '302a300506032b6570032100'; xxd -p -c 999; } | xxd -r -p > "$work/public.der"
openssl pkeyutl -verify -pubin -inkey "$work/public.der" -keyform DER -rawin -in "$work/checksums" -sigfile "$work/checksums.sig" >/dev/null
mkdir -p "$bin_dir"
arch="$(uname -m)"; case "$arch" in x86_64) asset=sockt-linux-amd64;; aarch64|arm64) asset=sockt-linux-arm64;; *) echo "unsupported architecture: $arch" >&2; exit 1;; esac
curl --fail --location --proto '=https' --tlsv1.2 "$base/$asset" -o "$work/sockt"
expected="$(awk -v n="$asset" '$2==n {print $1}' "$work/checksums")"
actual="$(sha256sum "$work/sockt" | awk '{print $1}')"
[[ -n "$expected" && "$expected" == "$actual" ]] || { echo 'checksum verification failed' >&2; exit 1; }
install -m 0755 "$work/sockt" "$bin_dir/sockt"
echo "Sockt $version installed to $bin_dir. Ensure it is on PATH, then run: sockt"
