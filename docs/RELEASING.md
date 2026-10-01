# Releasing Sockt

Release authority is an annotated `vX.Y.Z` tag on protected `master`. Dispatch
`Release` **from master**, supplying the existing tag as `version`; do not dispatch
from the tag. The workflow checks the tag/commit relationship against the dispatch
commit, validates inputs, checks out the release commit for builds and uses the
reviewed dispatch commit for signing/deployment tooling. No tag is created by CI.

## Prerequisites and baseline

Do not activate production until Linux and Windows CI pass and the deployment
runbook has been reviewed on a disposable staging host. Configure protected master
and tag rules, review workflow changes, and require approval on the
`production-release` environment. Protect the environment so only master can deploy.

Set the **existing** `SOCKT_UPDATE_PUBLIC_KEY` as a repository Actions variable so
unprivileged builds can embed it. If an environment variable overrides it, it must
match. Configure `SOCKT_SIGNING_KEY`, `HETZNER_DEPLOY_SSH_KEY`, and
`HETZNER_SSH_KNOWN_HOSTS` only as protected-environment secrets, and
`HETZNER_DEPLOY_HOST` / `HETZNER_DEPLOY_USER` as environment variables. The signing
key is the existing hexadecimal Ed25519 seed; never generate a new production key
for this repair. The workflow proves the private/public pair matches, but an
operator must establish that this is the key trusted by existing installations.

If no tags exist, verify which commit produced the currently deployed v0.9.2.
`726fa31` is the repository's v0.9.2 Windows updater commit; confirm that provenance
before annotating it. After approval, these are manual preparation steps, **not
commands executed by the repair tests**:

```sh
git tag -a v0.9.2 726fa31 -m "Verified existing v0.9.2 baseline"
# Review and explicitly approve publishing this baseline tag before pushing it.
```

After reviewed fixes are merged into master and CI passes, `sockt-release minor`
prints the candidate `v0.10.0`. Review changes, annotate the intended full commit,
and separately approve pushing the tag and dispatching Release. The helper does
not create/push tags or deploy. With no baseline tag it fails with instructions.
The workflow requires the annotated v0.9.2 baseline and conservatively compares
migrations against it, including when intervening releases were skipped.

## Naming and signing contract

- Tags, build identity and GitHub Releases: `v0.10.0`.
- Client manifest version and updater comparison: `0.10.0`.
- Checksum files: `sockt-v0.10.0-checksums.txt` and `.txt.sig`.
- Existing updater URL: `/updates/sockt-v0.10.0-sockt-linux-amd64`, etc.
- Public installer bundle: `/updates/releases/v0.10.0/`.
- Optional GitHub installer base: `/releases/download/v0.10.0/`.

Installers accept either `0.10.0` or `v0.10.0`, reject duplicate prefixes, and emit
one canonical tag. They default to public update-host assets so a private GitHub
repository does not break installation. `-Repository owner/repo` (Windows) or
`SOCKT_REPOSITORY=owner/repo` (Linux) explicitly selects public GitHub Releases.
Neither installer requests GitHub credentials.

Checksum signatures are **raw 64-byte Ed25519** for Go/OpenSSL interoperability.
The existing manifest signature stays hex-encoded JSON to preserve installed
client compatibility. Checksums authenticate binaries, helper, archive, installer
scripts, manifest, release identity and migration plan. Only a verified snapshot
is used by the privileged deployment wrapper. Signed checksum files are validated
before any uploaded server executable runs, and release code never runs as root.

Both installers require a trusted OpenSSL 3 installation; no nonexistent .NET
Ed25519 API is assumed. Windows supports Windows PowerShell 5.1 and PowerShell 7,
updates `%LOCALAPPDATA%\Sockt`, verifies both binaries before replacement, and
removes the unreleased competing `Sockt\bin` executables after a successful update.
It never launches a possibly shadowed `sockt` command automatically.

## Tests and commit completeness

See [RELEASE_TESTS.md](RELEASE_TESTS.md) for exact local/CI commands, scope and
remaining runner checks. Commit the Python, shell, PowerShell and Go test helpers
with the workflow changes. Committing only `.github/workflows` caused the missing
script failures: the implementations were still untracked in the working tree.

A successful server deploy and a successful publication are separate stages. A
retry preserves the original rollback executable. Immutable artifacts cannot be
overwritten with different bytes. If GitHub Release creation succeeded but final
publication failed, review the existing release and resume only the publication
step; do not delete/replace published assets or blindly rerun release creation.
