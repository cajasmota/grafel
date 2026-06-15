# Skills — agent guide

Each skill is a focused, independently-invokable capability published to agent hosts as `/skill-name`. Skills are markdown-first; the runtime reads `SKILL.md` and any per-pass prompts under `prompts/`.

## File conventions

- One directory per skill: `skills/<skill-name>/`
- Required: `SKILL.md` with YAML frontmatter (`name`, `description`, optional `args`)
- Optional siblings:
  - `prompts/` — per-pass prompt fragments referenced from `SKILL.md`
  - `templates/` — output templates the skill fills in
  - `conventions/` — shared rules referenced from multiple passes
  - `examples/` — worked examples shown to the user
  - `snippets/` — reusable prose fragments
  - `output-templates/` — final emitted artefacts

## Naming

- Use the `grafel-<concern>` prefix for skills that operate on an grafel-indexed group (e.g. `grafel-tech-docs`, `grafel-security-audit`).
- Skills that are agent-host utilities (not grafel-specific) drop the prefix (e.g. `extend-convention`, `using-grafel`).
- The skill ID must match the directory name and the frontmatter `name`.

## Installation

Skills are picked up from `skills/<name>/SKILL.md` by the agent host. There is no separate install step in this repo — landing a new directory ships the skill.

## When to update vs create

- **Update an existing skill** if the new behaviour fits the skill's stated scope (check the frontmatter `description`). Most additions should be updates.
- **Create a new skill** only when the concern is genuinely separable — a new skill earns its keep if it can be invoked without the others.
- The skill chain in `skills/README.md` shows the canonical execution order; new skills must declare where they slot in.

## Coverage matrix update

Skill changes typically do not touch the capability matrix — skills are agent-facing tooling, not extraction code. If a skill change exposes a new grafel CLI verb or MCP tool that materially changes capability surface, follow the root `AGENTS.md` "Coverage matrix update" workflow.
