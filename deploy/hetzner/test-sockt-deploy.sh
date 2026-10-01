#!/usr/bin/env bash
set -euo pipefail
# Called by Linux CI; the signer is built unprivileged first.
: "${1:?pass the absolute path of the test signer binary}"
exec python3 "$(dirname "$0")/test_deploy.py" "$1"
