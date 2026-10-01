#!/usr/bin/env bash
# Source only from reviewed workflow/build scripts. Values never become shell code.
set -euo pipefail
validate_release() {
  [[ ${RELEASE_TAG:-} =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'invalid release tag' >&2; return 2; }
  [[ ${APPLY_MIGRATIONS-false} == true || ${APPLY_MIGRATIONS-false} == false ]] || { echo 'migration approval must be true or false' >&2; return 2; }
  [[ -z ${BACKUP_ID:-} || ${BACKUP_ID} =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$ ]] || return 2
  [[ ${APPLY_MIGRATIONS:-false} != true || -n ${BACKUP_ID:-} ]] || { echo 'backup receipt required' >&2; return 2; }
  [[ ${APPLY_MIGRATIONS:-false} != false || -z ${BACKUP_ID:-} ]] || { echo 'backup ID requires migration approval' >&2; return 2; }
}
# Separate from tag validation: the build job resolves a validated tag first,
# then validates the resulting full SHA before checkout or passing it to jobs.
validate_commit() {
  [[ ${RELEASE_COMMIT:-} =~ ^[0-9a-f]{40}$ ]] || { echo 'full lowercase 40-character commit SHA required' >&2; return 2; }
}
validate_key() {
  [[ ${SOCKT_UPDATE_PUBLIC_KEY:-} =~ ^[0-9a-fA-F]{64}$ && ! ${SOCKT_UPDATE_PUBLIC_KEY} =~ ^0+$ ]] || { echo 'valid existing Ed25519 public key required' >&2; return 2; }
}
validate_remote() {
  [[ ${DEPLOY_USER:-} =~ ^[a-z_][a-z0-9_-]{0,31}$ && ${DEPLOY_HOST:-} =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$ ]] || { echo 'invalid deployment destination' >&2; return 2; }
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  validate_release
  validate_commit
fi
