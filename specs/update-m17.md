# Release signing and update manifest, M17

`goje package -sign KEYFILE` signs a release with the developer's Ed25519 key, and the package [`update`](../update/update.go) lets a game verify that a newer build is genuine and download it. [RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md) records why. The [build specification](build-m15.md) defines the release directory and the keys that `goje keygen` writes.

## What is signed

A signed release holds two signatures by the same key:

- `SHA256SUMS.sig` is the 64-byte Ed25519 signature of the exact bytes of `SHA256SUMS`. It lets a player check a download with standard tools, for example `openssl pkeyutl -verify -pubin -inkey KEY.pub -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig` with OpenSSL 3.
- `update.json` binds the game name and version to the same file sizes and digests. `SHA256SUMS` names no version that a signature covers, because a version in a file name can be renamed. A game must therefore trust only `update.json`, never a version read from a file name or from an unsigned place.

## update.json

`update.json` is UTF-8 JSON with exactly these members, in this order, indented by two spaces as Go's `json.MarshalIndent` writes them, and ending in one newline:

| Member | Value |
| --- | --- |
| `format` | The string `goje-update-v1`. |
| `name` | The game name: `[a-z][a-z0-9-]*`, at most 32 bytes. |
| `version` | The version, by the same rule as `goje -version`. |
| `files` | 1 to 64 objects, sorted by `name` in strictly increasing byte order, each with `name`, a base name of 1 to 128 bytes of letters, digits, `.`, `_`, and `-` whose first byte is a letter or digit; `size`, the exact size in bytes from 0 to 4 GiB; and `sha256`, the digest as 64 lowercase hexadecimal digits. |
| `signature` | The 64-byte Ed25519 signature, in standard Base64 with padding. |

The signed message is the compact JSON of the same object without `signature`: `{"format":"goje-update-v1","name":…,"version":…,"files":[{"name":…,"size":…,"sha256":…},…]}`. The rules above leave no character that needs escaping, so this text is unique. The `format` member keeps the message distinct from `SHA256SUMS` and from any later format.

`goje package` lists every archive of the release, the same files as `SHA256SUMS`, and not `SHA256SUMS`, its signature, or `update.json`.

## Verification

`update.Verify(data, publicKey)` accepts `update.json` only if all of these hold, and returns its name, version, and files:

- The public key has 32 bytes, and `data` has at most 64 KiB.
- `data` decodes, has the format `goje-update-v1`, and meets every rule of the table.
- `data` is exactly the bytes that `update.Sign` writes for the decoded content and signature. An unknown member, a duplicate or differently cased key, other spacing, or a missing final newline is rejected, so no byte of an accepted manifest is outside the signature.
- The signature verifies over the signed message with the public key.

One key can sign several games, so the caller checks the returned name against its own. `update.ParsePublicKey` reads the PEM public key that `goje keygen` writes, for a game to embed.

`update.Newer(manifest, current)` reports whether the manifest's version is later than `current` by [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html) precedence: the three numbers compare numerically; a version with a pre-release part is earlier than the same version without one; pre-release identifiers compare one by one, numeric ones numerically and before alphanumeric ones, which compare in byte order; and a longer list of equal identifiers is later. An equal or earlier version is not newer, so an old signed manifest cannot roll a game back. Both versions must match the `-version` rule.

`update.Download(ctx, client, url, file, w)` sends a GET request, requires status 200, rejects a declared `Content-Length` larger than `file.Size`, copies at most `file.Size` bytes to `w` while hashing them, and then fails if the body was shorter, has another byte, or has another SHA-256 digest. If it fails, `w` may hold part of an unverified file, which the caller must discard. A nil client means `http.DefaultClient`.

## Limits

- There is no expiry or timestamp, so a client offline from new releases cannot tell that it was shown an old manifest; it only refuses to treat it as newer.
- There is no key rotation or revocation. A lost key means a new key embedded in a new build.
- This format does not choose a host or install a download. The [M18 hosted channel](update-m18.md) builds on the same manifest wire format.
- Ed25519 signatures do not replace Authenticode or macOS notarization.
