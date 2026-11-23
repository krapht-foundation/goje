# Contributing to Goje

Goje has a Go module and a playable desktop and browser playground. Design feedback and documentation fixes are welcome. See the [project overview](docs/README.md) for the current scope.

## Propose and submit a change

1. Search the [issue tracker](https://github.com/krapht-foundation/goje/issues) before opening an issue. For a new subsystem, dependency, public API, or asset format, discuss the problem, alternatives, and desktop and web implications before implementing it.
2. Keep the change focused. Explain the user-facing problem, what changed, and what you verified. For performance claims, include the workload, target, and comparable before-and-after measurements.
3. Use the Go version in `go.mod` or a newer supported release. Run `make fmt` for formatting and `make check` for the read-only quality gate. For rendering, input, or other playground changes, run both smoke targets from the [playground guide](docs/playground.md) and name any target you could not verify.
4. Update [public documentation](docs/README.md) when behavior or usage changes. Use plain, complete sentences and distinguish existing features from plans. Use relative links within the repository and check that examples and commands match the current project.

GitHub Actions checks each pull request and each push to `main`. A new commit cancels older runs for the same pull request. The Ubuntu check runs both graphical smoke targets and caches the Chromium build selected by `package-lock.json`; it installs missing system libraries when the runner image does not provide them.

## Document code for maintainers

- Keep the code readable on its own. Use names that reflect the game or engine concept, keep ownership visible, and prefer straightforward control flow. Write comments in plain, complete sentences for a contributor who has not seen the original discussion.
- Give each engine package a package comment that tells readers what it owns, where it is used, and which responsibilities belong elsewhere. Use Go doc comments for exported APIs. Document observable behavior and, where relevant, units, ownership, lifetimes, call order, failure behavior, and thread or game-loop requirements. Add a short subsystem guide when the explanation spans packages or platforms; link to the relevant specification and decision rather than copying them.
- Explain the **why** next to the code that depends on it. A useful comment explains why the host drops overdue ticks after a stall or when a renderer may reuse a frame buffer. It does not narrate the loop or restate the assignment. Apply the same rule to build files, scripts, and tests: document non-obvious constraints, not routine syntax. Where an optimization changes how the code works, explain the tradeoff and link to measurements when available.
- Keep comments, examples, and design links current when behavior changes. Review documentation with the code: a new contributor should be able to find the entry point, understand the invariants, and verify a change without needing the original author's context.

## Script documentation

Use JSDoc in first-party JavaScript, including `.mjs` and inline scripts in HTML. Describe module purpose and document functions with parameter and return types and any relevant failure or ownership details. For Python, use PEP 257 docstrings for modules, classes, and functions, with type annotations on function signatures. Do not edit generated or vendored scripts to add documentation.

## Specs, decisions, and proposals

Record accepted behavior and verification criteria in `specs/`, and record architecture decisions with their rationale in `decisions/`. Use `rfcs/` when a proposal needs review before it becomes an accepted decision or specification. The [playground specification](specs/playground.md) and [first architecture decision](decisions/0001-first-runtime-and-tooling.md) are examples.

The [M1 runtime foundation RFC](rfcs/0002-m1-runtime-foundation.md) keeps the approved milestone plan and links to the accepted runtime specification and architecture decision. Follow that pattern rather than maintaining duplicate contracts.

By participating, you agree to the [code of conduct](CODE_OF_CONDUCTS.md). Report security concerns through the [security policy](SECURITY.md), not a public issue.
