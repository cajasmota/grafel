# Issue #60 — Relationship Emission Coverage Matrix

**Status:** Audit-only (matrix). Quick wins deferred to follow-up issues.
**Date:** 2026-05-10
**Branch:** `feat/issue-60-structural`
**Scope:** `internal/extractors/<lang>/` and `internal/extractors/cross/<feature>/`

## Method

For each extractor source file (excluding `_test.go`), grepped for both:

1. `RelationshipKind*` typed-constant references (the contract per `internal/types/kinds.go`).
2. Bare string literals matching the 21 known kinds (`CALLS`, `IMPORTS`, `EXTENDS`, `IMPLEMENTS`, `USES`, `USES_HOOK`, `CONTAINS`, `DEPENDS_ON`, `REFERENCES`, `ROUTES_TO`, `SERVES`, `PUBLISHES_TO`, `TESTS`, `HAS_PROPS`, `ACCESSES_TABLE`, `INJECTED_INTO`, `READS_FROM`, `WRITES_TO`, `RENDERS`, `RETURNS`, `TAGGED_AS`).

Cell legend: `Y` = emitted, blank = not emitted.

## Per-language extractor matrix

Languages with **at least one** kind emitted:

| Lang        | CALLS | IMPORTS | EXTENDS | IMPLEMENTS | CONTAINS | DEPENDS_ON | REFERENCES |
|-------------|:-----:|:-------:|:-------:|:----------:|:--------:|:----------:|:----------:|
| golang      |  Y    |   Y     |         |     Y      |    Y     |     Y      |            |
| java        |  Y    |   Y     |         |            |    Y     |            |            |
| javascript  |  Y    |   Y     |         |            |    Y     |            |            |
| kotlin      |  Y    |   Y     |         |            |    Y     |            |            |
| python      |  Y    |   Y     |         |            |    Y     |            |            |
| ruby        |  Y    |   Y     |         |            |    Y     |            |            |
| rust        |  Y    |   Y     |         |            |    Y     |            |            |
| php         |       |   Y     |         |            |    Y     |            |            |
| cpp         |       |   Y     |         |            |          |            |            |
| csharp      |       |   Y     |         |            |          |            |            |
| elixir      |       |   Y     |         |            |          |            |            |
| proto       |       |   Y     |         |            |          |            |            |
| scala       |       |   Y     |         |            |          |            |            |
| swift       |       |   Y     |         |            |          |            |            |
| hcl         |       |         |         |            |          |     Y      |            |
| markdown    |       |         |         |            |    Y     |            |     Y      |
| sql         |       |         |         |            |    Y     |            |     Y      |

Languages emitting **zero** relationships (entities only):

| Lang     | Notes |
|----------|-------|
| clojure  | Defines `SCOPE.Operation`/`SCOPE.Component` entities, no edges. |
| dart     | Defines class/method entities, no edges. |
| fish     | Function entities only. |
| groovy   | Class/method entities only — Grails/Gradle aware. |
| html     | Tag/component/UI entities only. |
| just     | Recipe entities only. |
| lua      | Function entities only. |
| razor    | UI/Component entities only. |
| shell    | Function entities only. |
| yaml     | Service/Config entities only (k8s/openapi/CI). |
| zig      | Function/struct entities only. |
| css      | Stylesheet/Component entities only. Note: `@import` not yet emitted as IMPORTS. |
| dockerfile | Component/Operation/Pattern/Schema entities only. |
| graphql  | Schema entities only. |

## Cross-cutting (`internal/extractors/cross/<feature>/`)

| Feature       | Kinds emitted              |
|---------------|----------------------------|
| dbmap         | `ACCESSES_TABLE`           |
| endpoint      | `SERVES`                   |
| hierarchy     | `EXTENDS`, `IMPLEMENTS`    |
| httpclient    | `CALLS`                    |
| imports       | `DEPENDS_ON`               |
| manifest      | `DEPENDS_ON`               |
| react_props   | `HAS_PROPS`, `RENDERS`, `USES_HOOK` |
| testmap       | `TESTS`                    |
| deprecation   | (no relationship; tagging only) |

The cross-cutting layer is the source of `EXTENDS`, `IMPLEMENTS`, `HAS_PROPS`,
`RENDERS`, `USES_HOOK`, `SERVES`, `TESTS`, `ACCESSES_TABLE`, and most
inter-package `DEPENDS_ON` edges. Per-language extractors are **not** expected
to duplicate this work — the audit treats cross-feature emission as the
authoritative source for those kinds.

## Kinds with no producer

The following typed constants exist but were not found at any producer call
site during the audit (excluding `_test.go`):

- `RelationshipKindUses` (`USES`)
- `RelationshipKindRoutesTo` (`ROUTES_TO`)
- `RelationshipKindPublishesTo` (`PUBLISHES_TO`)
- `RelationshipKindInjectedInto` (`INJECTED_INTO`)
- `RelationshipKindReturns` (`RETURNS`)
- `RelationshipKindTaggedAs` (`TAGGED_AS`)

`READS_FROM`/`WRITES_TO` are emitted by SQL but only via the `dbmap`
cross-cutting bridge (mapped from raw queries through resolvers); the SQL
extractor itself emits `CONTAINS` and `REFERENCES`.

## Gap summary vs. the v1.0 ship-gate spec

The spec (issue #60) calls out language-appropriate emission for **all 32**
extractors. Out of 28 language extractors plus 4 declarative/structural
extractors:

- **7** match the imperative `CONTAINS/CALLS/IMPORTS` triple
  (Python, TS/JS, Java, Kotlin, Ruby, Rust, Go).
- **8** emit only `IMPORTS` or `DEPENDS_ON` and are missing `CONTAINS`/`CALLS`
  (cpp, csharp, elixir, scala, swift, php, proto, hcl).
- **2** emit a partial declarative set (markdown: `CONTAINS`+`REFERENCES`;
  sql: `CONTAINS`+`REFERENCES`).
- **14** emit **no relationships at all** (clojure, dart, fish, groovy, html,
  just, lua, razor, shell, yaml, zig, css, dockerfile, graphql).

## Recommendation

Spec breakdown is correct: 14-day per-language rollout, time-boxed. This
matrix issue files individual follow-ups for each gap so they can be
prioritised against #58 (VERIFY-2 bug-rate matrix) data and #56 disposition
tagging. Quick wins are **not** included in this PR — every gap requires
either a non-trivial AST traversal or a cross-cutting decision (e.g. whether
yaml emits `DEPENDS_ON` from `needs:` keys), which exceeds the 30-min
exploratory cap on this audit.

## Follow-up issues

See linked sub-issues for each per-language gap; tracked under milestone
`1.0 — initial port`, label `bug,scope:indexer`, project board column `Todo`.
