# Releasing Sockt

Annotated tags (`vX.Y.Z`) are the release authority. A release workflow is
manually dispatched against an existing annotated tag and serializes production
runs. The protected `production-release` job alone receives the existing
`SOCKT_SIGNING_KEY`; it creates signed update metadata and release checksums.

Before enabling it, configure the GitHub environment, public-key variable,
signing-key secret, protected `master` rules, protected `v*` tags, and a review
requirement for workflow/script changes.

Configure production-release with secrets SOCKT_SIGNING_KEY, HETZNER_DEPLOY_SSH_KEY, and HETZNER_SSH_KNOWN_HOSTS; set non-secret variables SOCKT_UPDATE_PUBLIC_KEY, HETZNER_DEPLOY_HOST, and HETZNER_DEPLOY_USER. Tags must be annotated, match X.Y.Z, and be reachable from protected master.
