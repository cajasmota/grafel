# `<repo-slug>` configuration reference

> Every setting the repo reads at runtime. Alphabetized within each section.

## Environment variables

| Name | Required | Default | Type | Consumer | Purpose |
| ---- | -------- | ------- | ---- | -------- | ------- |
| `<NAME>` | yes\|no | `<default>` | string\|int\|bool\|json | `<entity>` | <one line> |

## Feature flags

| Flag | Default | Consumer | Purpose |
| ---- | ------- | -------- | ------- |

## Settings constants

| Name | File | Purpose |
| ---- | ---- | ------- |

## Configuration files

- `<file>` — <purpose> — <when read>.

## Secret sources

> Where secrets come from at runtime: AWS Secrets Manager, Vault, env vars from a sealed source, etc. One line per source.

## Known gaps

> List `grafel_enrichments(action=list, kind="env-var")` blockers here verbatim. Do not fabricate values.
