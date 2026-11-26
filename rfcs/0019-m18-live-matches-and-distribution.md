# RFC 0019: M18 — Live matches and distribution

**Status:** Accepted and verified for native desktop and web. Native mobile distribution is deferred.

## Problem and goal

[M17](0018-m17-versus-music-tools-and-reach.md#next-milestone) left live spectators and rejoin, a hosted update channel, and compressed music for M18. Outpost proves the online features, and Beacon proves the distribution path for native desktop and web, including offline web play.

## Decisions for review

| Decision | Recommendation | Alternative and tradeoff |
| --- | --- | --- |
| Spectators | Give a room one read-only spectator seat. The spectator catches up from the match's commands, then watches the live stream. | Add a third lockstep participant. A slow observer could stall play, and every session would have to support a non-player role. |
| Rejoin | Retain the two player slots through a 30-second transport interruption. Both players reconstruct the last mutually verified checkpoint before resuming. | Retain and resend every unacknowledged message to preserve the exact in-flight state. This needs reliable delivery across two connections and more state at the relay. |
| Distribution targets | Package native builds for macOS, Linux, and Windows and an installable web build with offline play. Preserve touch input in the browser. | Generate native Android and iOS projects. This would add separate build, signing, and verification pipelines before there is a requirement the web build cannot meet. |
| Hosted updates | Reuse the existing GitHub release workflow and the [M17 signed manifest](../specs/update-m17.md). Publish a Beacon channel before expanding it to more games. | Operate a new service. An update is a manifest and a set of immutable downloads; it does not need a server process. |
| Music | Start with mono 48 kHz, 16-bit [FLAC](https://www.rfc-editor.org/rfc/rfc9639.html). Decode before playback into the existing clip format. | Stream or use a different decoder on each host. That would change the real-time audio path; M18 needs smaller assets on disk, not a larger music memory budget. |
| Playable proof | Give Beacon a short light-the-beacons challenge with a clear win, timeout, and restart. Use the same challenge to verify desktop and web packages, music, and signed updates. | Build separate demos for each feature. Each would prove less and add another package to maintain. |

## Proposed design

### Outpost: watch and rejoin

The [relay](../specs/relay-m16.md) remains ignorant of game commands and state. A versioned room mode adds a read-only spectator route and time-limited slot reservations; the old routes retain their two-player behavior. The spectator code is separate from the player invitation. It grants a view, never a player seat. Each player receives a separate cryptographically random resume credential, kept in memory and presented as a control message over TLS, not in a URL or log. The relay authenticates the slot before forwarding anything, never replaces a connected player, and expires reservations. The official client attempts to rejoin after transport loss. A deliberate leave, a protocol failure, a desync, or an expired reservation ends the match rather than triggering rejoin. Browser refresh, process restart, and relay restart are not recoverable in this proposal.

Outpost reuses the [match log](../specs/match-log-v1.md) rules and command validators. The host sends a bounded prefix followed by incremental batches in tick order. The host publishes a batch only when both players have verified its ending checkpoint, so the view waits for checkpoint verification and rejoining cannot retract ticks the spectator has already seen. The spectator runs the same simulation, checks hashes, and can cycle the fog views without issuing commands. Serve catch-up with a per-update work budget and an independent bounded outbound queue. Disconnect a slow observer instead of making players wait. A recording can still be watched offline. A live spectator can inspect either side; invitation controls who may watch, not what a modified client can see.

When either player disconnects, both stop advancing and stop accepting orders while reconnecting. They agree on a checkpoint they both previously verified, including the initial state if no later checkpoint was verified. Each rebuilds locally from recorded commands, compares the resulting hash again, and starts a new lockstep epoch at that tick. Frames and pending orders from the previous epoch do not carry over. The UI states when play is paused and that orders after the resume point did not carry over. The replay records the surviving timeline once. If the players cannot establish a common checkpoint or the record exceeded its bounds, they end with a diagnostic and keep the playable prefix rather than accept another peer's snapshot. Set room, handshake, buffering, transfer, and catch-up limits in the accepted specifications before changing the wire. The existing [lockstep](../specs/lockstep-m16.md) and match log remain the determinism checks, not authentication of a peer or proof of who played.

### Hosted updates

The existing [release job](../.github/workflows/release.yml) collects native packages on the three desktop runners and already compares the Unix web archives. Generate one Beacon `update.json` over the final set of archives, after choosing the canonical copy of the web ZIP. Sign only in a protected release job, with a key whose public half the client obtains independently. Use a fixed channel URL for the manifest and fetch the selected archive from the version named by that signed manifest, not the moving channel URL. Keep the existing checksum and version rules. A native check-and-download workflow is sufficient: it verifies the signed game name, version, size, and digest, and exposes only a verified file to the player. Automatic installation and key rotation are out of scope.

Use a separately named staging channel and key to prove remote delivery before enabling the production channel. A local HTTPS fixture checks HTTP and signing logic but cannot establish that the hosted channel works. Publishing a tag or release and configuring signing secrets require separate maintainer authorization.

### Compressed music and Beacon

Evaluate a maintained pure-Go FLAC decoder, starting with [`mewkiz/flac`](https://github.com/mewkiz/flac), before adding a module dependency. Limit compressed bytes, metadata work, per-frame allocation, and decoded frames, and reject malformed or unsupported streams without touching the current track. A track uses the existing 64-second PCM limit. A limit in stream metadata alone is not sufficient; enforce it as frames are decoded. Decode during loading, not in the device callback, and do not add a second mixer or change the score format. Import a locally authored music loop and retain its source and conversion command. Compare its FLAC size with the same WAV, record decode time and peak allocation on desktop and WASM, and check that the decoded PCM is identical. This reduces packaged asset size, not the prepared clip's memory use.

Beacon uses a fixed board and fixed-tick timer. Light the marked beacons before the timer runs out, then offer restart after a win or timeout. Use keyboard and browser touch input. The same source and assets build for native desktop and web. The challenge does not require a network or an update check to start.

## Delivery and acceptance

Rejoin follows the spectator history path. Signing and hosting use the verified desktop and web packages. Beacon ties the distribution and music checks together.

| Slice | Exit check |
| --- | --- |
| Live spectator | Start a native Outpost host and a Chromium guest, then join from a second browser after play has started. Verify catch-up and a later shared state hash, switch fog, and prove that an observer's input or disconnect does not change the match. Run the relay checks under the race detector; exercise slow clients and caps. |
| Rejoin | Cut the host and guest transports separately during native/browser play. Verify that both keep their own slot, resume from the same checkpoint, and reach a later matching checkpoint and a playable saved replay. Cover expiry, stale frames, wrong credentials, early disconnect, and deliberate leave. |
| Desktop and web packages | Build and run Beacon for the host desktop and the web. Serve the packaged web build, then verify that it launches offline from its service worker. |
| Hosted updates | On a real HTTPS staging channel, verify a newer Beacon manifest and one native archive from the published version. Reject an older manifest, a wrong key, and a changed archive without leaving an unverified file. Verify that publishing another version does not make the previously signed artifact URL resolve to the new archive. |
| Compressed music | Round-trip Beacon's authored loop against its reference PCM, exercise the play and fade path on desktop and Chromium, and fuzz the bounded decode boundary. Report asset size and prepare time without changing the mixer allocation contract. |
| Playable Beacon | Win, time out, and restart the same board on desktop and browser, and complete it by touch in the browser. Check that music has no effect on the result. |

### Verification to date

- **Outpost:** A native host and Chromium guest resumed at tick zero after an early cut. A second Chromium page caught up, switched fog, and verified shared hashes after later cuts to each player. Both match logs replayed. Co-op and the injected-desync check also passed. A native relay test verified that a live spectator received the natural final tick before the room ended.
- **Beacon and FLAC:** Desktop and Chromium passed win, timeout, and restart; Chromium also passed the touch path. The installed web build opened with its server and network down. The decoder reproduced the authored PCM exactly and the asset is 44.7% of the equivalent WAV. The [decoder specification](../specs/music-flac-m18.md#measurements) records the timing and heap growth.
- **Hosted updates:** The protected release job published immutable Beacon prereleases [0.0.1-m18.1](https://github.com/krapht-foundation/goje/releases/tag/beacon-staging-v0.0.1-m18.1) and [0.0.1-m18.2](https://github.com/krapht-foundation/goje/releases/tag/beacon-staging-v0.0.1-m18.2). The corrected [staging smoke](../specs/update-m18.md#verification) passed from a local checkout against the live HTTPS channel. It verified the second signed manifest and Linux archive, verified the first manifest and archive at their original URLs, and rejected rollback, a wrong key, and substituted archive bytes without leaving an unverified file. The first release's manifest and Linux archive digests matched those recorded before the second publication. No production channel is enabled.

## Out of scope

Matchmaking, accounts, chat, more than two players, spectator voting or orders, server-authoritative simulation, prediction, resume after a restart, encrypted fog, and installing an update automatically. Native Android and iOS distributions, including wrappers and store signing or submission, are deferred. Authenticode, macOS notarization, streaming music, resampling, and additional codecs are also out of scope.

## Next milestone

M19 rehearses a Beacon preview release for native desktop and web on real machines, an HTTPS origin, and an independent trial. See [RFC 0020](0020-m19-release-rehearsal.md).
