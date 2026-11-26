# RFC 0020: M19 — Release rehearsal for desktop and web

**Status:** Proposed.

## Problem and goal

[M18](0019-m18-live-matches-and-distribution.md) proved the packaging, signing, and update path with Beacon, but it did so on hosted CI runners, a local web server, and a staging channel that only the maintainers use. The [building guide](../docs/building.md#limits) already records two gaps: Windows x64 was verified with Mesa software OpenGL rather than on a physical machine, and no one other than the maintainers has followed the documented workflow. Nothing yet shows that a downloaded Beacon starts on a player's computer, that an installed web build picks up a new version, or that the release procedure can be repeated by someone who did not write it.

M19 rehearses a public preview of Beacon for native desktop and web without publishing one. Its outcome is evidence: a written release procedure that was followed end to end, a record of what happened on real machines, and a go or no-go list for the first production channel.

## Decisions for review

| Decision | Recommendation | Alternative and tradeoff |
| --- | --- | --- |
| Scope | Add no engine or gameplay features. Fix only what the rehearsal exposes in packaging, updates, the service worker, and the documentation. | Combine the rehearsal with new features. A failure would be harder to attribute, and the milestone would no longer end in a release decision. |
| Real machines | Check unsigned CI artifacts on physical macOS, Windows x64, and Linux with a hardware OpenGL driver first, because they leave nothing permanent behind. Stage a signed version only after they pass, then repeat the checks on the browser download. Record the operating system, GPU, and result for each. | Stage first. A published tag and immutable release can never be reused, so every failed attempt would consume a version. Rely on hosted runners alone. They cannot show driver, window-system, or first-run behavior, which is what a player meets first. |
| Web hosting | Serve a staged web build from a real HTTPS origin and verify install, offline launch, and the switch to a newer version for a returning visitor. | Keep verifying against a local server. It cannot show how an origin, its cache headers, and an existing service worker cache interact. |
| Independent trial | Ask someone who did not write Goje to build, package, and update a small game from a fresh checkout, using only the documentation. Treat every question they ask as a documentation defect. | Run the steps again ourselves. We already know the undocumented parts, so the check would pass. |
| Code signing | Do not add Authenticode or macOS notarization in M19. Measure the first-run prompts on real machines and decide with that evidence. Purchasing a signing identity needs separate maintainer authorization. | Add signing now. It requires paid identities, secret handling, and a new protected workflow before there is evidence that the prompts block anyone. |
| Production channel | Write the enabling procedure and the go or no-go list. Leave the production channel disabled. | Enable it at the end of the rehearsal. Publishing needs a separate maintainer decision, as in M18. |

## Proposed design

### Real-machine smoke

The release workflow's build job already produces Beacon archives for each desktop system and the web build, without signing or publishing anything. First download those CI artifacts, launch them on each system, and play a round. The shipped archives do not contain Beacon's smoke host, which is behind build tags, so a person plays a round by hand with the keyboard, and in the browser by touch, to check win, timeout, and restart. A checkout of the commit can also run the native smoke on the same machine. When the artifacts pass, stage a version through the protected workflow, check the published archive against the signed manifest with `goje update`, and repeat the checks on the file a browser downloads from the release page. Record how the system reacted to an unsigned program, including the macOS **Open Anyway** step and Windows SmartScreen, so that the signing decision has data. These checks are manual because the hosted runners lack the hardware, and the rehearsal records their results in the procedure rather than adding unattended jobs.

### Web update behavior

Publish the web package behind HTTPS on a staging path that is not linked from the project README. Verify that the first visit installs the service worker, that a repeat visit works with the network off, and that publishing a newer version reaches a returning visitor without clearing site data.

The version change itself is checked locally by the Beacon build smoke, which publishes a second bundle at the same origin. The [service worker](../specs/build-m15.md) does not take over open pages, so a controlled page keeps its version, even across a reload, until every tab or window has closed, and the next page then receives the new version. That is intentional, because replacing the cache under a running game could mix two builds. The [building guide](../docs/building.md) now says that reloading is not enough. The hosted check confirms that a real origin and its cache headers do not change this behavior; it does not need a fix unless it does.

### Release procedure

Write a human-facing [runbook](../runbook/release.md) that covers choosing a version, staging, verifying on real machines, checking the web update, deciding go or no-go, and rolling back by publishing a newer fixed version, since a signed channel never accepts an older one. It links to the specifications for the rules and states which steps require maintainer authorization. The independent trial starts from the runbook and the guides, not from a conversation with the authors.

### Evidence record

Record results in this RFC's verification section in the same form as M18: the machine, the operating system and version, the graphics hardware, the archive digest checked, and the outcome. Do not claim a platform that has no entry. Where a machine is unavailable, state that the platform remains verified only on the hosted runner.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| Native machines | On physical macOS, Windows x64, and Linux, the CI artifact launches and completes Beacon's win, timeout, and restart paths. The staged archive then passes `goje update` verification, launches from a browser download, completes the same paths, and exits cleanly. The first-run prompts and workarounds are recorded for each system. |
| Web origin | On a real HTTPS origin, the web build installs, launches with the network off, and a returning visitor receives a newer published version. The staged path is removed after the check. |
| Independent trial | A person outside the project builds and packages a small game, signs a manifest, and verifies an update using only the documentation. Every blocking question becomes a documentation change or a defect. |
| Procedure | A maintainer follows the runbook to stage a new Beacon version without help from its author, and publishes the go or no-go list. |
| Regression | `make check`, the race tests, and `make smoke-goje-build` still pass, and the hosted CI jobs pass on the commit. |

### Verification to date

| Source | System | Hardware | Archive digest | First-run prompts | Result |
| --- | --- | --- | --- | --- | --- |
| CI artifact, [run 45863820117](https://github.com/krapht-foundation/goje/actions/runs/45863820117), commit `b00a86f` | macOS 15.5, arm64, MacBook | Apple M4 Pro, built-in speakers | `sha256:bd9aaa115ab2887a6f99c87e0ed2b8190fb5fae62f9b8400065257e805e7c453` | None. The archive was downloaded with `gh` and extracted with `tar`, so it carried no download mark; the Gatekeeper path is not yet checked. | Launched, played, and exited with status 0. Audio was clean. |

The rehearsal found two defects that the hosted runners could not show, and both are fixed on `main`:

- **Deployment target.** The first macOS artifact was built on a macOS 26 runner, and its executable required macOS 26.0 although `Info.plist` promised 12.0. macOS 15.5 refused to open it with "You can't use this version of the application with this version of macOS." The desktop build now sets the deployment target to the same `12.0`, and the Beacon build smoke compares the two. See the [build specification](../specs/build-m15.md).
- **Audio period.** miniaudio's default 10 ms device period crackled on the built-in speakers although every callback arrived on time and took under 150 µs. A 40 ms period played cleanly. See the [mixer specification](../specs/audio-mixer-m15.md#hosts).

Windows x64, Linux, the web origin, the staged release, the first-run prompts, and the independent trial have no entries yet. Those platforms remain verified only on hosted runners.

## Out of scope

Native Android and iOS distributions, Authenticode, macOS notarization, installers, automatic installation of a verified update, key rotation, a production channel, new gameplay, and new engine features. Signing can enter a later milestone if the recorded first-run behavior shows that players cannot reasonably get past it.
