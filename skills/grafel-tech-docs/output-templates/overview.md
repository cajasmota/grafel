# `<repo-slug>` overview

> One paragraph: what this repo does and why it exists. Lift from `domain.md` and from the most central entities discovered in Pass 1. No marketing copy — this is for engineers.

## Architectural skeleton

> One paragraph naming the dominant pattern (request/response, worker pool, IaC stack-set, etc.) and the two or three central abstractions in backticks.

## Modules

| Module | What it does | Doc |
| ------ | ------------ | --- |
| `<module-slug>` | <one line> | [`modules/<module-slug>/README.md`](./modules/<module-slug>/README.md) |

> Module slugs must match `plan.json` exactly. Listed in dependency order — modules with no upstream module first.

## Entry points

- `<file:line>` — `<entity>` — <role>

> Pulled from the convention's `entry_points` section. Every entity in backticks.

## Public API surface

Summary only — full listing lives in [`reference/api.md`](./reference/api.md).

- `<n>` HTTP routes
- `<n>` CLI commands
- `<n>` exported functions/classes
- `<n>` background tasks

## Configuration

Summary only — full listing lives in [`reference/config.md`](./reference/config.md).

> One paragraph naming the configuration mechanism (env vars, settings files, CLI flags) and where the canonical schema lives.

## Connections to other repos

> Each accepted cross-repo edge from `grafel_cross_links(action=list)` (status=accepted) goes here as one bullet:
> - `<this entity>` → `<other repo>:<entity>` via <method>.

## Pending links

> Each candidate (status=pending) goes here under a callout:
>
> > These are unconfirmed. Pass 8 will resolve them; do not rely on them.
>
> - `<this entity>` → `<other repo>:<entity>?` via <method>.

## How to develop locally

See [`how-to/local-dev.md`](./how-to/local-dev.md).

## Glossary

See [`glossary.md`](./glossary.md).
