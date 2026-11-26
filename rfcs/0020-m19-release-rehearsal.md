# RFC 0020: M19 — Release rehearsal for desktop and web

**Status:** Proposed.

## Problem and goal

[M18](0019-m18-live-matches-and-distribution.md) proved the packaging, signing, and update path with Beacon, but it did so on hosted CI runners, a local web server, and a staging channel that only the maintainers use. The [building guide](../docs/building.md#limits) already records two gaps: Windows x64 was verified with Mesa software OpenGL rather than on a physical machine, and no one other than the maintainers has followed the documented workflow. Nothing yet shows that a downloaded Beacon starts on a player's computer, that an installed web build picks up a new version, or that the release procedure can be repeated by someone who did not write it.

M19 rehearses a public preview of Beacon for native desktop and web without publishing one. Its outcome is evidence: a written release procedure that was followed end to end, a record of what happened on real machines, and a go or no-go list for the first production channel.

## Decisions for review

| Decision | Recommendation | Alternative and tradeoff |
| --- | --- | --- |
| Scope | Add no engine or gameplay features. Fix only what the rehearsal exposes in packaging, updates, the service worker, and the documentation. | Combine the rehearsal with new features. A failure would be harder to attribute, and the milestone would no longer end in a release decision. |
| Real machines | Run the Beacon smoke on physical macOS, Windows x64, and Linux with a hardware OpenGL driver, using the archives that the staging workflow produced. Record the operating system, GPU, and result for each. | Rely on hosted runners. They cannot show driver, window-system, or first-run behavior, which is what a player meets first. |
| Web hosting | Serve a staged web build from a real HTTPS origin and verify install, offline launch, and the switch to a newer version for a returning visitor. | Keep verifying against a local server. It cannot show how an origin, its cache headers, and an existing service worker cache interact. |
| Independent trial | Ask someone who did not write Goje to build, package, and update a small game from a fresh checkout, using only the documentation. Treat every question they ask as a documentation defect. | Run the steps again ourselves. We already know the undocumented parts, so the check would pass. |
| Code signing | Do not add Authenticode or macOS notarization in M19. Measure the first-run prompts on real machines and decide with that evidence. Purchasing a signing identity needs separate maintainer authorization. | Add signing now. It requires paid identities, secret handling, and a new protected workflow before there is evidence that the prompts block anyone. |
| Production channel | Write the enabling procedure and the go or no-go list. Leave the production channel disabled. | Enable it at the end of the rehearsal. Publishing needs a separate maintainer decision, as in M18. |

## Proposed design

### Real-machine smoke

The staging workflow already publishes immutable Beacon archives for each desktop system and the web build. For each system, download the published archive as a player would, check it against the signed manifest with `goje update`, extract it, and launch it. Beacon already exposes a smoke mode for win, timeout, and restart; run it on the real display and also play one round by hand with keyboard, and in the browser with touch. Record how the system reacted to an unsigned program, including the macOS **Open Anyway** step and Windows SmartScreen, so that the signing decision has data. These checks are manual because the hosted runners lack the hardware, and the rehearsal records their results in the procedure rather than adding unattended jobs.

### Web update behavior

Publish the web package behind HTTPS on a staging path that is not linked from the project README. Verify that the first visit installs the service worker, that a repeat visit works with the network off, and that publishing a newer version reaches a returning visitor without clearing site data. If the current service worker can keep serving an outdated build indefinitely, fix that in the [shell](../cmd/goje/shell/index.html) and extend the existing offline smoke to cover the version change. Keep the behavior consistent with the [M15 build specification](../specs/build-m15.md).

### Release procedure

Write a human-facing runbook in `runbook/` that covers choosing a version, staging, verifying on real machines, checking the web update, deciding go or no-go, and rolling back by publishing a newer fixed version, since a signed channel never accepts an older one. It links to the specifications for the rules and states which steps require maintainer authorization. The independent trial starts from the runbook and the guides, not from a conversation with the authors.

### Evidence record

Record results in this RFC's verification section in the same form as M18: the machine, the operating system and version, the graphics hardware, the archive digest checked, and the outcome. Do not claim a platform that has no entry. Where a machine is unavailable, state that the platform remains verified only on the hosted runner.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| Native machines | On physical macOS, Windows x64, and Linux, the staged archive passes `goje update` verification, launches, completes Beacon's win, timeout, and restart paths, and exits cleanly. The first-run prompts and workarounds are recorded for each system. |
| Web origin | On a real HTTPS origin, the web build installs, launches with the network off, and a returning visitor receives a newer published version. The staged path is removed after the check. |
| Independent trial | A person outside the project builds and packages a small game, signs a manifest, and verifies an update using only the documentation. Every blocking question becomes a documentation change or a defect. |
| Procedure | A maintainer follows the runbook to stage a new Beacon version without help from its author, and publishes the go or no-go list. |
| Regression | `make check`, the race tests, and `make smoke-goje-build` still pass, and the hosted CI jobs pass on the pull request. |

## Out of scope

Native Android and iOS distributions, Authenticode, macOS notarization, installers, automatic installation of a verified update, key rotation, a production channel, new gameplay, and new engine features. Signing can enter a later milestone if the recorded first-run behavior shows that players cannot reasonably get past it.
