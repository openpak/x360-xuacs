# Deploy: x360-xuacs

## Configuration

Environment `X360_XUACS_*` (see `deploy/primary/openpak-x360-xuacs.container` and `x360-common/config`): `ADDR`, `STATE`, `RSA_KEY`, `TLS_ADDR`, `INTERNAL_ADDR`, `INTERNAL_KEY`, `ACCOUNT_CORE_URL/TOKEN`, `DEBUG`.

## Build and test

`TMPDIR=<scratch> X360_KDC_TESTKEYS=<dir with the dev keys> go test -mod=vendor ./...`

Tag `vX.Y.Z` and CI publishes `ghcr.io/openpak/x360-xuacs:latest` (amd64 + arm64); hosts auto-update `:latest`.
`deploy/` holds the live quadlet and helpers. History before 2026-10-18 is the x360-kdc monorepo's, filtered to
this service. Licensed AGPL-3.0, see LICENSE.

## Shared library

The shared Go library [`x360-common`](https://github.com/openpak/x360-common) is consumed with `replace` and a committed `vendor/`. After changing x360-common, run `go mod vendor` here and commit.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` with `Authorization: Bearer $ACCOUNT_ERASE_KEY` on a separate listener `ERASE_ADDR` (default `:9099`, container network only, never published; not served while the key is unset). Reply `{"service":"x360-xuacs","rows":N}`; one `account erased` log line per erasure.

Website step name: `x360-xuacs`.

Deletes the `accounts` record (online XUID, console profiles), the account's `tokens` and `standing` in the `state` document. Kept: nothing per account. The `account_deleted` event runs the same purge (own keys only). **Order: run this step last**; the other x360 services find the account's XUIDs through `POST /internal/v1/account-xuids`. Note `RunReconcile` re-creates a profile for any account the core lists as active, so call this only for accounts that are closed in the core (deletion pending/deleted).
