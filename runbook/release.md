# Verify and stage a Beacon preview release

## When to use

Use this procedure to check a Beacon build on real machines and then, only if it passes, publish it on the staging channel. It covers native desktop builds for macOS, Linux, and Windows, and the web build. It does not cover native Android or iOS, code signing, or the production channel. See [RFC 0020](../rfcs/0020-m19-release-rehearsal.md) for the reasons.

The procedure has two stages. The first uses unsigned continuous-integration artifacts, which leave nothing permanent behind, to find problems cheaply. The second signs and publishes an immutable prerelease, and only then checks the update path and the first-run behavior a player sees. Stage a version only after the first stage passes, because a published tag and release can never be reused.

You need:

- Maintainer permission on `krapht-foundation/goje` to run the [release workflow](../.github/workflows/release.yml). For staging, you also need permission to push tags and a maintainer who is a required reviewer of the `beacon-staging` environment. The reviewer approves the signing job; the person who starts the run can be the same person.
- The Go toolchain named in `go.mod` and the GitHub CLI, signed in, on the machine where you run the commands below.
- For the real-machine checks, one physical macOS, one Windows x64, and one Linux machine with a hardware OpenGL driver, and a browser on each.
- For the web check, an HTTPS origin and path you control that is not linked from the project README.

Running the build workflow only builds and uploads artifacts. Publishing a tag or release, uploading to a host, and any change to secrets, variables, or environments require explicit maintainer authorization. Do not continue past a step marked **Authorization** without it. This runbook never enables a production channel.

Set these once. The public key is the half the maintainers configured as the `BEACON_STAGING_PUB` repository variable; a client must obtain it independently of the files it verifies.

```sh
REPO=krapht-foundation/goje
CHANNEL=https://raw.githubusercontent.com/$REPO/refs/heads/beacon-staging-channel/update.json
ARTIFACTS="https://github.com/$REPO/releases/download/beacon-staging-v{version}/{file}"
```

## Procedure

### 1. Build the candidate

1. Start from the commit you intend to ship, on `main`, with every required check green.
2. Run the build and nothing else. With no inputs the workflow builds on all three desktop systems and does not sign, tag, or publish.

   ```sh
   gh workflow run release.yml --repo "$REPO" --ref main
   gh run list --workflow release.yml --repo "$REPO" --limit 1   # note the run ID
   gh run watch RUN_ID --repo "$REPO"
   ```

3. Download the artifacts. Each one holds that system's archives for the playground and Beacon, named `goje-beacon-0.0.0-dev-PLATFORM.tar.gz` (`.zip` on Windows). The web ZIP comes from the Ubuntu artifact; the Windows job does not upload one. GitHub keeps artifacts for 90 days by default.

   ```sh
   gh run download RUN_ID --repo "$REPO" --dir candidate
   ls candidate/*/goje-beacon-*
   ```

   Record the commit and run ID. A development build is not signed, so its version is not a version of the channel.

### 2. Verify the candidate on each native system

Repeat on each machine. The platform names are `darwin-arm64`, `linux-amd64`, and `windows-amd64`; copy the Beacon archive for that system to the machine, or run the download there.

1. Extract the archive and start the game. Record whether it opens, and any operating system prompt. A copy that `gh` downloaded has no download mark, so this shows launch behavior, not the first-run prompts. To see those early, download the same artifact from the run's web page in a browser and extract it with the system's default extractor.
2. Play Beacon by hand with the [play checklist](#play-checklist).
3. Record the machine, operating system and version, graphics hardware and driver, the archive's SHA-256 digest, and the outcome.

To run the automated native checks on the same machine, use a checkout of the commit with the Go toolchain, Node, and Playwright, as `make smoke-goje-build` does. The shipped archives are built without Beacon's smoke host, which is behind build tags, so they cannot run it. The scripts are Bash; on Windows, run them from Git Bash. This has been run on macOS and on hosted runners only.

#### Play checklist

Confirm each of these, which the [Beacon specification](../specs/beacon-m18.md#rules) defines:

- The window title starts with `Goje Beacon`, and three blue beacons appear on the board.
- A click or tap lights a beacon. Arrow keys or **WASD** move the tomato, and **Space** lights the beacons above it.
- Lighting all three beacons shows `ALL LIT! TAP OR R`. Starting a round and then lighting nothing for 10 seconds shows `TIME UP! TAP OR R`.
- **R**, **Enter**, **Space**, or a click restarts after the result, and the beacons are unlit again.
- Music plays, quiets after a win or timeout, and returns on restart. Sound effects play.
- Closing the window ends the process without an error message. Run it once from a terminal; the exit status should be 0.

### 3. Verify the candidate's web build

**Authorization:** ask the maintainer before uploading to any host.

1. Extract `goje-beacon-0.0.0-dev-web.zip` from the Ubuntu artifact and upload the files to the staging path of your HTTPS origin.
2. In a fresh browser profile, open the page. Check that the game loads and plays by keyboard, and by touch on a touch device, following the play checklist. Check that the browser offers to install the app.
3. Close and reopen the page with the browser's network disabled. The game must start from the cache.
4. For the update check, first upload an older bundle, such as the previous staged version's web ZIP, visit the page once, and reload it once so that the service worker controls the page. Then upload this candidate's files over it. Visit again; the old version should keep running, even after a reload. Close every tab and window of the page, open it again, and confirm that the new version is served. A reload alone is not enough; this is documented in the [building guide](../docs/building.md#what-you-get).
5. Record the browser, version, operating system, origin, and result. Remove the staged path when you finish.

### 4. Decide whether to stage

Stage only if the candidate launched and completed the play checklist on all three systems and in the web build, and no failure is unexplained. Fix problems on `main` and build a new candidate instead of staging a known defect; staging is permanent.

### 5. Stage the version

**Authorization:** ask the maintainer to approve staging this version.

1. Find the version on the channel now, and choose a [semantic version](https://semver.org/) that is strictly newer. A signed channel never accepts an equal or older version. Versions in the `0.x.y` range are appropriate until the project has a stable release. Record the version you are replacing as `PREVIOUS`.

   ```sh
   gh variable get BEACON_STAGING_PUB --repo "$REPO" > beacon-staging.pub
   go build -o goje ./cmd/goje
   ./goje update -manifest "$CHANNEL" -key beacon-staging.pub -name goje-beacon -current 0.0.0
   ```

2. Tag the commit you verified and start the run. Pushing the tag does not start anything by itself.

   ```sh
   VERSION=0.1.0; PREVIOUS=0.0.1-m18.2
   git tag "beacon-staging-v$VERSION" COMMIT
   git push origin "beacon-staging-v$VERSION"
   gh workflow run release.yml --repo "$REPO" --ref "beacon-staging-v$VERSION" \
     -f stage_version="$VERSION" -f previous_version="$PREVIOUS"
   ```

3. The `package` jobs build again on all three desktop systems. The `stage` job then waits for a `beacon-staging` reviewer. The reviewer should confirm that the run was started on the expected tag and approve it in the Actions UI. Only this job can use the signing key.
4. Wait for `package`, `stage`, and `verify-stage` to pass. `verify-stage` is the [staging smoke](../specs/update-m18.md#verification) over real HTTPS. It checks the new version, `PREVIOUS`, rollback refusal, a wrong key, and a substituted archive.

If the run fails before the prerelease exists, read the failure, and delete the tag with `git push origin :"refs/tags/beacon-staging-v$VERSION"` only if `gh release view "beacon-staging-v$VERSION" --repo "$REPO"` reports that no release exists. If a release exists, never reuse the version; go to [Recovery](#recovery-and-escalation).

### 6. Verify the staged release

Repeat on each machine, using the platform names above. Windows archives are `.zip`; the others are `.tar.gz`. Build `goje` there with the Go toolchain, or copy a binary you built for that system.

1. Download the archive the way a player would, with a browser, from the release page `https://github.com/krapht-foundation/goje/releases/tag/beacon-staging-v$VERSION`. This gives the file the download mark that triggers the operating system's first-run checks.
2. Download and verify the same archive with the updater, then compare digests. The updater accepts the file only if the signed size and SHA-256 digest match.

   ```sh
   FILE=goje-beacon-$VERSION-PLATFORM.tar.gz
   mkdir verified
   ./goje update -manifest "$CHANNEL" -key beacon-staging.pub -name goje-beacon \
     -current 0.0.0 -artifacts "$ARTIFACTS" -file "$FILE" -o verified
   shasum -a 256 verified/"$FILE" path/to/browser-download/"$FILE"
   ```

   Use `sha256sum` where `shasum` is missing and `Get-FileHash` in PowerShell. The two digests must be identical.
3. Extract the browser-downloaded archive with the system's default extractor, so that the download mark carries over, and start the game as a player would, from Finder, Explorer, or the file manager. Record what the system did on the first launch. On macOS, a `.app` that is not signed or notarized stops at Gatekeeper until the player chooses **Open Anyway**. On Windows, SmartScreen may warn about an unsigned executable. Record each prompt and the exact steps that got past it, or that it could not be passed.
4. Run the [play checklist](#play-checklist) again on the staged build.
5. Record the machine, operating system and version, graphics hardware and driver, the archive digest, each first-run prompt, and the outcome.

For the web build, take `goje-beacon-$VERSION-web.zip` from the verified download, so that it is the signed file, and repeat the web checks in step 3 with it, using the previous version's ZIP as the older bundle.

### 7. Decide go or no-go

Answer each question in the evidence record below. Every answer must be yes for a go. A no for any item is a no-go, and the follow-up work belongs in the next milestone's RFC.

- Did `package`, `stage`, and `verify-stage` pass, and were the required checks green on the staged commit?
- Did the staged archive launch and complete the play checklist on each of macOS, Windows x64, and Linux, with a recorded browser-download digest equal to the verified digest?
- Is every first-run prompt documented in the [building guide](../docs/building.md#limits), with a way past it, or a recorded decision that it blocks players?
- Did the web build install, work offline, and replace itself after every window closed, on a real HTTPS origin?
- Did someone outside the project follow the guides to build, package, sign, and verify a small game, and are all of their blocking questions fixed?
- Has a maintainer who did not write this milestone followed this runbook and staged a version?
- Is there no unresolved private security report affecting a staged artifact?

A go does not enable the production channel. That needs a separate proposal that names the key, environment, URL, and approvers, and a separate maintainer authorization.

## Verification

A completed run leaves these facts, which a reviewer can check without trusting the person who ran it:

- A prerelease `beacon-staging-v$VERSION` with the four archives, `SHA256SUMS`, and `update.json`, marked immutable by GitHub:

  ```sh
  gh release view "beacon-staging-v$VERSION" --repo "$REPO" --json isImmutable,isPrerelease,assets \
    --jq '{isImmutable,isPrerelease,assets:[.assets[].name]}'
  ```

- A channel manifest that verifies with the public key and names `$VERSION`, from the command in step 5.
- The `verify-stage` job log for the run, ending with `update smoke: goje-beacon $VERSION verified`.
- The candidate's run ID and commit, and one evidence entry per machine and per browser, in the form below, added to the RFC that governs the milestone. Do not claim a system that has no entry.

| Source | System | Hardware and driver | Archive digest | First-run prompts | Result |
| --- | --- | --- | --- | --- | --- |
| Run ID or staged version | macOS version, arm64 | GPU | `sha256:…` | Gatekeeper steps | Checklist outcome and exit status |
| Run ID or staged version | Windows version, x64 | GPU and driver | `sha256:…` | SmartScreen steps | Checklist outcome and exit status |
| Run ID or staged version | Linux distribution, x64 | GPU and driver | `sha256:…` | Desktop file and permission steps | Checklist outcome and exit status |
| Run ID or staged version | Browser and version, operating system | Not applicable | `sha256:…` of the web ZIP | Install prompt | Offline start, version change after closing all windows |

## Recovery and escalation

- **A bad version is published.** A signed channel accepts only newer versions, and a published tag and immutable release cannot be reused. Fix the problem on `main`, choose a new, higher version, and run the procedure again. Clients that already verified the bad version stay on it until they check again. Do not delete the bad release as a rollback; deleting it can make that version unavailable but does not make its tag reusable, and the channel will still name a version that no longer downloads until you publish the fix.
- **The web build is bad.** The service worker installs a new worker whenever any file differs, not only when the version is newer, so re-upload the previous bundle to roll the web build back. Players receive it once every window of the page has closed.
- **A candidate fails.** Nothing is published, so fix `main`, build a new candidate, and start again. Artifacts expire on their own.
- **A staging step failed.** Stop, keep the logs and the output of the command, and do not retry with a different key, repository, or tag. If a job failed after publishing a prerelease but before moving the channel, the channel still names the earlier version; stage a newer version.
- **A signing key may be exposed.** Stop all staging. Do not use or print the key. The channel has no key rotation or revocation, so report the exposure to the maintainers privately following [SECURITY.md](../SECURITY.md) and wait for a decision before staging again.
- **You found a vulnerability, or a verification you expected to fail succeeded.** Do not post it publicly. Follow [SECURITY.md](../SECURITY.md).
- **The documentation is wrong or missing.** Record the exact step where you were blocked and what you tried, then change the document or open an issue. An independent trial's blocking question is a defect, not a failure of the reader.
