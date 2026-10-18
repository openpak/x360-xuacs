# x360-xuacs

The Xbox 360 account service (Live service 5 and its siblings served from one HTTP backend): `/xuacs/*.ashx` (sign-in, profile
recovery), the Windows Live ID login (`nexus.passport.com`, `login.live.com`, TLS), `/xsig` certificate validation,
`/xedl` CountryInfo, `/xpnfront/xpresence.srf` (presence alive), `/xbos/GetRevocationList.ashx` and the `/xstats/*` replies.
Also the internal user-key API the KDC uses for the user AS exchange.

Part of OpenPak's Xbox 360 stack: one repository and one image per service ([`x360-kdc`](../x360-kdc),
[`x360-gateway`](../x360-gateway), [`x360-xuacs`](../x360-xuacs), [`x360-liveinfo`](../x360-liveinfo),
[`x360-notice`](../x360-notice), [`x360-xexds`](../x360-xexds)) around the shared Go library
[`x360-common`](../x360-common) (`replace` + committed `vendor/`; after changing x360-common run
`go mod vendor` here and commit).


## Configuration

Environment `X360_XUACS_*` (see `deploy/primary/openpak-x360-xuacs.container` and `x360-common/config`): `ADDR`, `STATE`, `RSA_KEY`, `TLS_ADDR`, `INTERNAL_ADDR`, `INTERNAL_KEY`, `ACCOUNT_CORE_URL/TOKEN`, `DEBUG`.

## Build and test

`TMPDIR=<scratch> X360_KDC_TESTKEYS=<dir with the dev keys> go test -mod=vendor ./...`

Tag `vX.Y.Z` and CI publishes `ghcr.io/openpak/x360-xuacs:latest` (amd64 + arm64); hosts auto-update `:latest`.
`deploy/` holds the live quadlet and helpers. History before 2026-10-18 is the x360-kdc monorepo's, filtered to
this service. Licensed AGPL-3.0, see LICENSE.
