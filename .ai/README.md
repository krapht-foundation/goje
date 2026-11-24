# Agent workspace

[`AGENTS.md`](../AGENTS.md) is the portable entry point. `.ai/` owns reusable agent material; it links to project documentation, never replaces it. The engine and toolchain must not depend on this directory or any agent host.

| Location | Owns |
| --- | --- |
| [`README.md`](../README.md) | Project scope and current status |
| [`docs/`](../docs/) | Public guides and product documentation |
| [`specs/`](../specs/) | Accepted behavior, interfaces, and invariants |
| [`decisions/`](../decisions/) | Accepted architecture decisions and their rationale |
| `rfcs/` | Proposals under discussion and their outcomes |
| `runbook/` | Human-facing development and operational procedures |
| [`memories/`](memories/) | Agent orientation and handoffs, not project contracts |
| [`skills/`](skills/) | Repeatable workflows, one `<name>/SKILL.md` per skill |
| [`agents/`](agents/) | Reusable role prompts, one Markdown file per role |
| [`templates/`](templates/) | Authoring templates, not active instructions |

Create project documentation directories when there is material to put in them. For a task, follow explicit instructions and applicable accepted contracts; inspect code and configuration for actual behavior. If sources disagree, show the discrepancy rather than settling it from memory.

## Tool adapters

| Host | Instructions | Skills | Roles |
| --- | --- | --- | --- |
| Codex / AGENTS.md-compatible tools | `AGENTS.md` | `.agents/skills` | Read `.ai/agents/` directly |
| Claude Code | `CLAUDE.md` imports `AGENTS.md` | `.claude/skills` | `.claude/agents` |
| OpenCode | `AGENTS.md` | `.opencode/skills` | `.opencode/agents` |
| OMP | `AGENTS.md` | `.omp/skills` | `.omp/agents` |

The skill and role adapters are relative symlinks into `.ai/`. Edit the canonical files and keep `CLAUDE.md` as a one-line import. Do not link entire tool directories: tool-specific settings and runtime state must stay out of the shared knowledge layer. Do not commit local settings, credentials, transcripts, or generated state. On checkouts that cannot materialize symlinks, read the canonical files directly rather than maintaining copies.

## Start here

- [Workspace memory](memories/workspace.md) — how to resume work in a fresh session.
- [Change reviewer](agents/goje-change-reviewer.md) — review scope, regressions, and platform coverage.

## Authoring templates

- Use the [agent](templates/agent.md) and [skill](templates/skill.md) templates for reusable roles and workflows.
- Use the [memory](templates/memory.md) template for durable orientation and [working memory](templates/working-memory.md) for active handoffs.
- Use the [spec](templates/spec.md), [ADR](templates/adr.md), [RFC](templates/rfc.md), and [runbook](templates/runbook.md) templates for documents in their corresponding repository directories, not under `.ai/`.

## Technical skills

| Skill | Load for |
| --- | --- |
| [Gameplay](skills/goje-gameplay/SKILL.md) | Input, game rules, reproducible scenarios, and RTS commands |
| [Engine development](skills/goje-engine-development/SKILL.md) | Frame timing, 2.5D rendering, spatial queries, and platform boundaries |
| [Content toolchain](skills/goje-content-toolchain/SKILL.md) | Editors, asset formats, and author-to-engine round trips |
| [Go 1.27+](skills/goje-golang/SKILL.md) | Go APIs, memory ownership, concurrency, and tests |
| [Performance](skills/goje-performance/SKILL.md) | Profiling, benchmark design, and optimization |
| [Security](skills/goje-security/SKILL.md) | Defensive review, trust boundaries, untrusted content, and dependencies |
| [Knowledge sync](skills/goje-knowledge-sync/SKILL.md) | Reconciling public docs, decisions, procedures, and agent context |

## Default modes

[Ponytail](skills/ponytail/SKILL.md) keeps design and implementation focused. [Caveman](skills/caveman/SKILL.md) keeps internal chat concise. [`AGENTS.md`](../AGENTS.md#default-modes) enables both without host-specific plugins and excludes all public and persisted text from Caveman.
