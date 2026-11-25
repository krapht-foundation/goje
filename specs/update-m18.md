# Hosted update channel, M18

M18 defines how to publish signed Beacon releases on a hosted staging channel, and provides a native command that checks it and downloads a verified archive. [RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md#hosted-updates) records why. The manifest wire format, the signed message, and the version rules are those of the [M17 update specification](update-m17.md), unchanged.

## Channel layout

A channel is a fixed manifest URL and a pattern of versioned archive URLs, both HTTPS. Beacon uses a dedicated GitHub branch for the moving manifest and immutable GitHub Releases for versioned files:

| Item | URL |
| --- | --- |
| Channel manifest | `https://raw.githubusercontent.com/krapht-foundation/goje/refs/heads/beacon-staging-channel/update.json` |
| Archive | `https://github.com/krapht-foundation/goje/releases/download/beacon-staging-v{version}/{file}` |
| Version manifest | `https://github.com/krapht-foundation/goje/releases/download/beacon-staging-v{version}/update.json` |

The `beacon-staging-channel` branch serves `update.json`, which each publication replaces. A published `beacon-staging-vVERSION` prerelease holds that version's four archives, `SHA256SUMS`, and its own copy of `update.json`. GitHub locks the assets and tag when the release is published. A client takes the version from the signed manifest, never from the moving channel URL, so publishing a later version cannot make an earlier manifest name a different archive.

## Signing a channel manifest

`goje manifest -name NAME -version V -sign KEYFILE -pub PUBFILE [-platforms LIST] [-o FILE] ARCHIVE...` writes one `update.json` over the combined archives of a release built on several hosts. It fails without writing anything unless all of these hold:

- The private key in `KEYFILE` has the public key in `PUBFILE`. The public key is configured separately from the private key, so a replaced or wrong secret fails closed.
- The base names of the archives are exactly `NAME-V-web.zip` and, for each platform in the comma-separated `LIST`, `NAME-V-PLATFORM.tar.gz`, or `NAME-V-PLATFORM.zip` for a `windows-` platform. A missing, extra, repeated, or differently named or versioned archive is rejected.
- The written manifest verifies with the public key.
- `FILE`, `update.json` by default, does not exist.

## Release workflow

The [release workflow](../.github/workflows/release.yml) drafts an unsigned release when a `v*` tag is pushed. A push never signs or publishes the staging channel. To stage a version, create a `beacon-staging-vVERSION` tag on the commit to package, run the workflow by hand on that tag, and set `stage_version` to `VERSION`. The `stage` job then:

1. Waits for approval in the protected `beacon-staging` environment, which holds the signing key.
2. Fails unless `stage_version` equals the tag's version and both the key and its configured public half are set.
3. Requires the Ubuntu and macOS web archives to be identical and takes the Ubuntu copy as canonical.
4. Signs the web, `linux-amd64`, `darwin-arm64`, and `windows-amd64` archives with `goje manifest`, deleting the key file when the step ends.
5. Creates the `beacon-staging-vVERSION` prerelease with all archives and the manifest, and only then commits the new manifest to `beacon-staging-channel`.

The `verify-stage` job, which has no access to the key, runs `scripts/smoke-update.sh` against the published channel. The optional `previous_version` input names an earlier staged version whose manifest and archive must still verify.

The maintainer configures, once, outside the repository:

- A `beacon-staging-channel` branch for `update.json`. It can start from the default branch; the workflow only writes `update.json` to it.
- The environment `beacon-staging`, with required reviewers and a deployment rule that allows only `beacon-staging-v*` tags.
- The environment secret `BEACON_STAGING_KEY`, the PEM private key from `goje keygen -o beacon-staging`.
- The repository variable `BEACON_STAGING_PUB`, the matching PEM public key. Players obtain the same public key from the maintainer or the game build, not from the channel.

## Checking and downloading

`goje update -manifest URL -key PUBFILE -name NAME -current V [-artifacts PATTERN -file FILE -o DIR]` is the native check-and-download workflow. It fetches the manifest from the fixed HTTPS URL, reading at most 64 KiB, and fails unless it verifies with the key in `PUBFILE`, names `NAME`, and has a version newer than `V`. Without `-o` it prints the verified version and its files. With `-o` it downloads `FILE` from `PATTERN`, an HTTPS URL in which `{version}` becomes the signed version and `{file}` the file name, and saves it in the existing directory `DIR`. It installs and runs nothing.

The package [`update`](../update/channel.go) provides the same steps to a game:

- `update.Check` fetches and verifies a manifest, checks its name, and reports whether it is newer. It accepts only `https` URLs and refuses a redirect to another scheme.
- `update.FileURL` finds a named file in a verified manifest and builds its versioned URL. A pattern without exactly one `{version}` and one `{file}`, or a name the manifest does not list, is an error.
- `update.Save` downloads into a temporary file in the target directory, verifies its exact size and SHA-256 digest with `update.Download`, syncs it, and publishes it under its signed base name with a hard link that fails if the name exists. It removes the temporary file in every case, so a failed or tampered download leaves no file and an existing file is never replaced.

## Verification

`go test ./update ./cmd/goje` uses generated keys and local HTTPS servers to show that a newer manifest and one archive verify, and that a wrong key, a wrong game name, an equal or older version, a changed manifest or archive, a missing archive, plain HTTP, and an existing output file are refused without leaving a file. It also shows that `goje manifest` refuses a mismatched key and any archive set other than the required one.

A localhost fixture cannot show that the hosted channel works. `scripts/smoke-update.sh PUBKEY VERSION [PREVIOUS]` checks over HTTPS that the channel names `VERSION`, one signed native archive downloads and verifies, and an equal version, a wrong key, a duplicate download, and an unlisted archive are refused without leaving a new file. With `PREVIOUS`, the older version's signed manifest and archive still verify, the older manifest is refused as an update, and the newer manifest rejects the older archive's bytes without leaving a file. Neither published archive is modified for that check. `REPO`, `NAME`, and `PLATFORM` select another repository, game, or archive.

## Limits

- Staging and enabling a production channel require separate maintainer authorization. M18 does not enable a production channel.
- Keep GitHub's immutable releases setting enabled. Deleting a release can still make a version unavailable; it cannot make its tag reusable.
- The M17 limits remain: no expiry, no key rotation or revocation, and no automatic installation.
