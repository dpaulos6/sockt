#!/usr/bin/env bash
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/sockt-deploy"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
for args in 'deploy --version nope --commit abcdef0 --sha256 00' 'deploy --version v1.2.3 --commit nope --sha256 00'; do
  if bash "$script" $args 2>/dev/null; then echo "invalid input accepted" >&2; exit 1; fi
done
grep -Fq -- '--allow-migrations' "$script"
grep -Fq 'flock -n' "$script"
grep -Fq 'stable.json.next' "$script"
