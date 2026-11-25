# Security policy

## Supported versions

Goje has not published a supported engine or toolchain release. The security reporting channel is open for concerns about this repository and its tooling. Do not rely on development builds for production use.

## Report a vulnerability privately

Email the [Krapht Foundation contact](mailto:mostafa.sh.coderino@gmail.com) with `Goje security` in the subject. Do not post the report, supporting files, or private project data in a public issue, discussion, or pull request.

Include the affected revision or version, platform, a description of the impact and conditions, and a safe way to verify the issue. Redact credentials and personal data. Contact us before sending sensitive attachments so we can agree on a secure transfer method.

Maintainers will triage reports privately and coordinate fixes and disclosure with the reporter. If you find a problem in a dependency, notify its maintainers through their security channel as well. Do not access other people's data or disrupt services while investigating.

## Online play

Online play treats the other player and the relay as untrusted. The [WebSocket subset](specs/websocket-m16.md), the [relay](specs/relay-m16.md), and the [lockstep protocol](specs/lockstep-m16.md) bound every size, rate, and wait, and reject malformed input with an error instead of acting on it. A match applies only commands that name units their sender owns, and it ends when the players' state hashes differ. The relay forwards messages without encrypting or authenticating them, so serve it behind TLS and do not treat a room code as a secret beyond the few minutes in which a room waits for its guest. Report a way to crash, stall, or desynchronize a player or the relay privately.

The [match log](specs/match-log-v1.md) and [music score](specs/music-m17.md) are untrusted, bounded inputs. Match playback validates commands and ownership again. A replay checkpoint is a determinism check, not proof of authenticity. A lockstep player runs the entire simulation; fog hides information in the normal interface but does not protect it from a modified client. Lockstep alone also cannot authenticate the source of commands forwarded by a relay.

The [M18 relay](specs/relay-m18.md) admits one read-only spectator with its own invitation and authenticates returning players with a separate 256-bit credential per seat. A client keeps it in memory and presents it in the first control message over TLS, never in a URL or log. The relay bounds incomplete resume authentication by `-max-pending` and a 15-second deadline. A spectator can see both teams, and a peer or relay remains untrusted. [FLAC music](specs/music-flac-m18.md) is an untrusted asset with separate compressed, metadata, and decoded-size limits.

[Signed updates](specs/update-m17.md) require a developer public key obtained independently of the download. Keep private signing keys out of project roots, bundles, and logs. The updater verifies the signed metadata, version, size, and artifact digest; it does not install or run a download.

The [Beacon staging channel](specs/update-m18.md) signs only in a protected release environment. Do not expose its signing key to build or test jobs.
