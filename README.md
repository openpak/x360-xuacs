# Xbox 360 Account Server

OpenPak's Xbox 360 Account Server.

| | |
| --- | --- |
| Network | Xbox 360 |
| Role | Live service 5 account backend: sign-in, profile recovery, the Windows Live ID login, the user-key API for the KDC and the identity API for the other Xbox 360 services |
| Depends on | [`x360-common`](https://github.com/openpak/x360-common) (shared Go library), account core (`ACCOUNT_CORE_URL`) |
| Used by | [`x360-kdc`](https://github.com/openpak/x360-kdc) |
| Image | `ghcr.io/openpak/x360-xuacs` |
| Licence | AGPL-3.0-only |

## What it serves

| Endpoint | What |
| --- | --- |
| `/xuacs/*.ashx` | Sign-in, profile recovery |
| Windows Live ID login (`nexus.passport.com`, `login.live.com`) | TLS login |
| Internal API | User AS exchange for the KDC; online XUIDs, gamertags, console profiles and the WLID CA for x360-xpnfront, x360-xsig and x360-xstats |

Presence and friends, XStringVerify, CountryInfo, certificate validation, the revocation list and the
`/xstats` settings moved to their own services on 2026-11-01 (see [Protocol](docs/protocol.md)).

## Run

Runs as the `ghcr.io/openpak/x360-xuacs` container, configured by `X360_XUACS_*` environment variables.
See [Deploy](docs/deploy.md).

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)
- [Notes](docs/notes.md)

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
