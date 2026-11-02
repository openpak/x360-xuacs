# Protocol: x360-xuacs

The Xbox 360 account service, Live service 5: `/xuacs/*.ashx` (sign-in, profile recovery), the Windows Live ID
login (`nexus.passport.com`, `login.live.com`, TLS) and the internal API: the user-key exchange for the KDC, and
the identity data (online XUIDs, gamertags, console profiles, the WLID CA) for the other Xbox 360 services.

Split out on 2026-11-01 (v0.10.0), one repository per Live service: presence and friends (1) to
[x360-xpnfront](https://github.com/openpak/x360-xpnfront), XStringVerify (2) to
[x360-msgserver](https://github.com/openpak/x360-msgserver), CountryInfo (11) to
[x360-xedl](https://github.com/openpak/x360-xedl), certificate validation (12) to
[x360-xsig](https://github.com/openpak/x360-xsig), the revocation list (21) to
[x360-xbos](https://github.com/openpak/x360-xbos), and profile settings and the other `/xstats` replies (34, 35)
to [x360-xstats](https://github.com/openpak/x360-xstats).

Native service-5 opcode `0x63C` (`XeUserGetAgeGroup.ashx`) derives Child/Teen/Adult
from the active account's core birthday (under 13 / under 18 / 18 and older, UTC).
An uncollected birthday retains Child policy; malformed/future dates and core
failures return service unavailable. The requested XUID must be on the tunnel
ticket.

## Account events

The core's event stream (`Events/PollEvents`, same `ACCOUNT_CORE_URL`/`TOKEN`) is polled every 5 s (`internal/xuacs/events.go`). `account_banned` and `account_delete_started` deny the account: its session tokens are dropped, sign-in is refused, the KDC gets no user key for it and its WLID tickets stop verifying. `account_unbanned` and `account_restored` lift their own reason (a restored but banned account stays denied); a suspension keeps the online XUID, console profiles and gamertag held. `account_deleted` (final erasure) purges the account's profiles (online XUID, offline XUIDs, XenonKeys, gamertags) and tokens, which releases the XUID for reuse; the x360 links are erased by the core. The cursor is stored in `xuacs.json` with each effect, so redelivery is a no-op; a state without one replays from the start and converges on the same standing.

## Friends snapshot

Native Friends serves the initial accepted-friend/block snapshot. Friend-request mutations, live presence push, activity feed and Avatar Store are not served. The dashboard's online Social content package is not supplied by the server.