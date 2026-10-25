# Deploy: x360-xuacs

## Configuration

Environment `X360_XUACS_*` (see `deploy/primary/openpak-x360-xuacs.container` and `x360-common/config`): `ADDR`, `STATE`, `RSA_KEY`, `TLS_ADDR`, `INTERNAL_ADDR`, `INTERNAL_KEY`, `ACCOUNT_CORE_URL/TOKEN`, `DEBUG`.

## Build and test

`TMPDIR=<scratch> X360_KDC_TESTKEYS=<dir with the dev keys> go test -mod=vendor ./...`

Tag `vX.Y.Z` and CI publishes `ghcr.io/openpak/x360-xuacs:latest` (amd64 + arm64); hosts auto-update `:latest`.
`deploy/` holds the live quadlet and helpers. History before 2026-10-18 is the x360-kdc monorepo's, filtered to
this service. Licensed AGPL-3.0, see LICENSE.
