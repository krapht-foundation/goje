# Working on goje

Start with [the project scope](README.md) and [the agent workspace map](.ai/README.md). Load only the memories, skills, and roles relevant to the task. Paths in agent roles and skills are relative to the repository root unless noted.

Load the relevant [technical skills](.ai/README.md#technical-skills) before design, implementation, or review. Use Go 1.27 or newer for Go changes, following the module's version floor once one exists.

## Default modes

- [Ponytail](.ai/skills/ponytail/SKILL.md) is on for design and coding: choose the smallest maintainable change that fully solves the task.
- [Caveman](.ai/skills/caveman/SKILL.md) is on for internal chat only. It is **off for public-facing docs and all persisted or external text**, including drafts: use complete sentences in `README.md`, `docs/`, contribution and policy files, issues, PRs, commits, and code comments.
- Read the mode files when first applicable in each session, even when the host does not auto-load them. Honor explicit user requests to turn a mode off. The public-text exception always applies.

## Working rules

- The project targets both desktop and web. Do not treat a desktop-only assumption as a shared engine contract. When changing shared behavior, verify it on both targets or state which target remains unverified.
- Keep gameplay behavior separate from platform details. Read the relevant accepted specs and ADRs before changing behavior or structure. The [first architecture decision](decisions/0001-first-runtime-and-tooling.md) documents the current module and playground backend; it does not select an editor toolkit or asset format.
- Match the scope of the request. Preserve unrelated changes, run the narrowest check that exercises your work, and report what actually ran. Do not stage, commit, push, or publish without an explicit request.
- Keep project requirements and decisions outside `.ai/`. Edit the canonical source rather than restating it in agent instructions or memory. When code and an accepted contract disagree, flag the drift; do not silently redefine the contract to match the implementation.
- Follow [CONTRIBUTING.md](CONTRIBUTING.md) for contribution and verification expectations and [SECURITY.md](SECURITY.md) for private disclosure. Treat imported projects and assets as untrusted.
