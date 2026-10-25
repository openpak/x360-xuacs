# Notes: x360-xuacs

Part of OpenPak's Xbox 360 stack: one repository and one image per service ([`x360-kdc`](../../x360-kdc),
[`x360-gateway`](../../x360-gateway), [`x360-xuacs`](../../x360-xuacs), [`x360-liveinfo`](../../x360-liveinfo),
[`x360-notice`](../../x360-notice), [`x360-xexds`](../../x360-xexds)) around the shared Go library
[`x360-common`](../../x360-common) (`replace` + committed `vendor/`; after changing x360-common run
`go mod vendor` here and commit).

## Account events

The core's event stream (`Events/PollEvents`, same `ACCOUNT_CORE_URL`/`TOKEN`, no new variable) is polled
every 5 s (`internal/xuacs/events.go`). `account_banned` and `account_delete_started` deny the account:
its session tokens are dropped, sign-in is refused, the KDC gets no user key for it and its WLID tickets
stop verifying. `account_unbanned` and `account_restored` lift their own reason (a restored but banned
account stays denied); a suspension keeps the online XUID, console profiles and gamertag held.
`account_deleted` (final erasure) purges the account's profiles (online XUID, offline XUIDs, XenonKeys,
gamertags) and tokens, which releases the XUID for reuse; the x360 links themselves are erased by the core.
The cursor is stored in `xuacs.json` with each effect, so redelivery is a no-op; a state without one
replays from the start, which converges on the same standing.

## Native Friends snapshot

This implements the initial accepted-friend/block snapshot. Friend-request
mutations, live friend-presence push updates, activity feed and Avatar Store
remain separate work. A server-side snapshot does not restore the dashboard's
missing online Social content package, nor establish physical-console parity.
