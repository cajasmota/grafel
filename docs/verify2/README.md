# VERIFY-2 corpus harness

Two sibling scripts run the bug-rate / resolution-rate measurement
harness against a curated corpus of public OSS repositories.

| script | entries | when to run | purpose |
| --- | ---: | --- | --- |
| `scripts/verify2/run.sh` | 114 | every change, CI ship-gate, nightly baseline | tier-1 — fast, covers the full 32-language extractor matrix without intra-family redundancy |
| `scripts/verify2/run-extended.sh` | 168 | pre-release, weekly cron | tier-2 — exhaustive, picks up framework-specific surfaces (Quarkus annotations, NgRx dispatchers, Liquibase XML changelogs, etc.) |

Both scripts share identical harness logic — clone-or-update,
sparse-checkout for monorepos, per-repo wall-clock cap, JSON-stats
aggregation, per-language bucketing, ship-gate check. They differ
only in the `REPOS` array.

## Rationale (2026-05-19 curation)

The previous combined manifest contained 283 entries (282 unique;
one duplicate name). Curation against `internal/extractors/`
demonstrated that the majority of entries were intra-family
redundancies — e.g. eight TypeScript state-management libraries
all exercising the same decorator + store-creation surface already
covered by `nestjs`, `nextjs-commerce`, `angular-realworld`, and
`sveltekit`.

The split applied here:

- **Single-source MUST-KEEP** — 22 entries that are the only corpus
  exercising their extractor or DSL (razor, fish, just, zig, lua,
  proto, graphql, notebook, sql_dbt, bicep, starlark, java_bpmn,
  smithy, avro, thrift, json-schema, asyncapi, raml, api-blueprint,
  nginx-conf, apache-httpd-conf, caddyfile, traefik-dynamic,
  kong-declarative, envoy-yaml, haproxy-cfg, selenium multi).
- **Language primary realworld apps** — one canonical realworld app
  per language extractor. These cover the framework + ORM + manifest
  + CI surface for their stack.
- **Secondary distinctive coverage** — one entry per ORM / message
  broker / IaC family / NoSQL ecosystem / IDL alternative not
  already covered by a primary.

Entries dropped from tier-1 fall into one of three categories:

1. **Framework alternates** (29 → 6 web frameworks): `axum` / `rocket`
   / `warp` / `tide` (rust) all exercise the same router-macro
   surface, so one Actix + one Axum suffice. Same collapse for
   Go alternates (`echox`, `gofiber`, `beego`) and Python
   alternates (`tornado`, `starlette`, `pyramid`, `bottle`).
2. **State management** (8 → 1): redux/mobx/zustand/pinia/ngrx/
   recoil/effector all single-language TS libraries; covered
   incidentally by realworld apps. Keep `xstate` for the
   distinctive statechart DSL.
3. **CDK/Pulumi multi-flavor sprawl** (9 → 2): per-language CDK
   variants test the per-language extractor, which is *already*
   exercised by a realworld app in that language. Keep one CDK
   (TypeScript) + one Pulumi (Go).

Plus the `esp-idf` entry (a pure-C corpus) was dropped from both
tiers — archigraph has no `c` extractor (only `cpp`, already
covered by `spdlog`). It would index zero files.

See `corpus-curation-2026-05-19.md` in this directory for the full
per-entry decision log (283 entries, each tagged KEEP-primary,
KEEP-secondary, or DROP with rationale).

## Per-run scratch dir behaviour

Per-repo JSON stats are written under
`$ARCHIGRAPH_CORPORA_DIR/_reports/<timestamp>-partial/` rather than
into `mktemp -d`. The `EXIT` trap only deletes that directory if
a `.complete` sentinel file exists, which the final aggregation
step writes on success. Aborts (SIGKILL, timeout, etc.) therefore
preserve the per-repo JSON so a partial run can be inspected or
resumed by hand. Operators should `rm -rf` stale `*-partial/`
directories during regular housekeeping.

## Hard rules

- Neither script writes inside the archigraph repo. Corpora and
  reports live under `$ARCHIGRAPH_CORPORA_DIR` (default:
  `$HOME/Documents/Projects/archigraph-corpora`).
- The on-disk corpora directory is not pruned by these scripts.
  Entries removed from a `REPOS` array are simply ignored at run
  time; their clone remains on disk for reference until an operator
  cleans it up.
- The harness logic is intentionally duplicated across the two
  scripts (rather than extracted to a shared library) because the
  duplication is small, the call site is one per script, and a
  shared library would force both scripts to track the same
  version of the harness in lockstep.
