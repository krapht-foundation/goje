# Contributing to Goje

Goje has a Go module and a playable desktop and browser playground. Design feedback and documentation fixes are welcome. See the [project overview](docs/README.md) for the current scope.

## Propose and submit a change

1. Search the [issue tracker](https://github.com/krapht-foundation/goje/issues) before opening an issue. For a new subsystem, dependency, public API, or asset format, discuss the problem, alternatives, and desktop and web implications before implementing it.
2. Keep the change focused. Explain the user-facing problem, what changed, and what you verified. For performance claims, include the workload, target, and comparable before-and-after measurements.
3. Use the Go version in `go.mod` or a newer supported release. Run `make fmt` for formatting and `make check` for the read-only quality gate. For rendering, input, or other playground changes, run both smoke targets from the [playground guide](docs/playground.md) and name any target you could not verify.
4. Update [public documentation](docs/README.md) when behavior or usage changes. Use plain, complete sentences and distinguish existing features from plans. Use relative links within the repository and check that examples and commands match the current project.

## Script documentation

Use JSDoc in first-party JavaScript, including `.mjs` and inline scripts in HTML. Describe module purpose and document functions with parameter and return types and any relevant failure or ownership details. For Python, use PEP 257 docstrings for modules, classes, and functions, with type annotations on function signatures. Do not edit generated or vendored scripts to add documentation.

## Specs, decisions, and proposals

Record accepted behavior and verification criteria in `specs/`, and record architecture decisions with their rationale in `decisions/`. Use `rfcs/` when a proposal needs review before it becomes an accepted decision or specification. The [playground specification](specs/playground.md) and [first architecture decision](decisions/0001-first-runtime-and-tooling.md) are examples.

By participating, you agree to the [code of conduct](CODE_OF_CONDUCTS.md). Report security concerns through the [security policy](SECURITY.md), not a public issue.
