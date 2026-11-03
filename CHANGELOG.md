# Changelog

- 2026-11-03: v0.11.2: No secrets in logs: `request` and `wlid request` no longer log the user agent, content type, schema header or body hex, and the debug response-body hex is gone. Test added.

- 2026-11-03: v0.11.1: Every request logs one stderr line, `http <method> <path> -> <status> <duration>ms` (x360-common v0.5.1 `httplog`): no query, headers, cookies, tokens or bodies; `/healthz` skipped.

- 2026-11-03: v0.11.0 state moves to Postgres (schema `x360_xuacs` in database `x360`, `X360_XUACS_DATABASE_URL`, required:
  the service refuses to start without it). An existing `xuacs.json` is imported once on first start and left in place.
  The RSA key and WLID key stay files in `X360_XUACS_STATE`.

- Unreleased: follow the account core's event stream: bans and started deletions drop the account's session tokens and refuse sign-in, user keys and WLID tickets; unban/restore lift them; final erasure purges its profiles and releases the XUID.

- 2026-11-01: v0.10.0 one repository per Live service: presence and friends (1), XStringVerify (2), CountryInfo
  (11), certificate validation (12), the revocation list (21) and the /xstats settings and stubs (34, 35) moved to
  x360-xpnfront, x360-msgserver, x360-xedl, x360-xsig, x360-xbos and x360-xstats; their Traefik routes now point
  there (replies checked byte for byte against v0.9.7 before the switch).

- 2026-11-01: v0.9.7 internal API for the services being split out of xuacs: identity, online XUID, XUID to
  account, console profiles and the WLID CA (client: x360-common xuacsapi).

- 2026-10-20: v0.9.3 acknowledge a departing profile after its active ticket is replaced, bound to its issuing console; log rejected presence status.

- 2026-10-20: v0.9.2 native age-group replies from account-core birthdays, ticket-bound policy requests, and empty native logoff acknowledgements.

- 2026-10-20: v0.9.1 native presence-alive friend/block snapshots from account core,
  versioned updates, ticket-user binding and discovery enforcement.

- 2026-10-18: v0.9.0 split out of x360-kdc v0.8.14 into its own repository and image (`ghcr.io/openpak/x360-xuacs`),
  history kept; shared packages now come from `x360-common`. Earlier entries: the x360-kdc CHANGELOG.
