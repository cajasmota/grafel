# `<repo-slug>` dependencies reference

> Direct dependencies only. Transitive dependencies appear in the lockfile and do not need to be listed here.

## Runtime dependencies

| Name | Version | Purpose |
| ---- | ------- | ------- |
| `<dep>` | `<pin>` | <one line> |

## Development dependencies

| Name | Version | Purpose |
| ---- | ------- | ------- |

## Peer dependencies

> Only for libraries; skip for applications.

| Name | Range | Purpose |
| ---- | ----- | ------- |

## Native / system dependencies

> System packages, native libraries, OS-level requirements. Skip if none.

- `<package>` — <why> — <how it gets installed>.

## Lockfile policy

> Which lockfile is the source of truth, who is allowed to update it, and how reproducibility is enforced.

## Vendored code

> Anything under `vendor/`, `third_party/`, or copied-and-modified upstream code. List it here so it does not get re-discovered as a "missing" public surface element.
