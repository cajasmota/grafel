# Extraction-quality benchmark framework

This is the framework for measuring **extraction quality** — orthogonal to
`bug-rate` (which lives under `internal/resolve/` and `docs/verify2/`).

## What it measures

| Metric | Question it answers |
|---|---|
| **Entity recall** | Did we extract every entity that SHOULD exist? |
| **Relationship recall** | Did we emit every relationship that SHOULD exist? |
| **Forbidden hits** | Did we emit a relationship that SHOULDN'T exist (false positive)? |
| **Nice-to-have** | Capabilities we want to track but won't fail CI on. |

bug-rate is "given an edge, is its Disposition correct?". A repo can score
`bug_rate=0%` while missing half of the real edges — bug-rate only grades
what was extracted, not what was missed. This framework closes that gap.

## Layout

```
internal/quality/
├── expected.go         # Fixture / ExpectedEntity / ExpectedRelationship types
├── diff.go             # Evaluate(fixture, doc) -> Report
├── report.go           # WriteHuman / WriteJSON
├── diff_test.go        # Unit tests for the matcher
└── golden/
    ├── python-django-mini/      # Python + Django
    ├── typescript-react-mini/   # TypeScript + React
    ├── java-spring-mini/        # Java + Spring Boot
    ├── go-chi-mini/             # Go + chi router
    └── rust-tokio-mini/         # Rust + tokio
        ├── src/        # Small hand-curated source tree
        └── expected.json
```

## Running

Single fixture:

```bash
build/grafel quality internal/quality/golden/python-django-mini
build/grafel quality --json out.json internal/quality/golden/python-django-mini
```

All fixtures (CI-shaped runner):

```bash
scripts/quality/run.sh
# writes one JSON report per fixture to reports/quality/
```

Exit codes:

| Code | Meaning |
|---|---|
| 0 | All must-have entities + relationships found, 0 forbidden hits |
| 2 | At least one must-have miss OR at least one forbidden hit |

## Adding a fixture

1. Create `internal/quality/golden/<name>/src/` and drop a small hand-curated
   source tree (5-10 files, ~20-50 entities, ~30-100 relationships).
2. Run the indexer once to see what it actually produces:
   ```bash
   build/grafel index --pretty --out /tmp/g.json internal/quality/golden/<name>/src
   jq -r '.entities[] | "\(.kind)\t\(.name)\t\(.source_file)"' /tmp/g.json | sort -u
   jq -r '.relationships[] | "\(.kind)\t\(.from_id)\t\(.to_id)"' /tmp/g.json
   ```
3. Author `expected.json` against the schema in `internal/quality/expected.go`.
   Use `must_exist: true` for the recall floor and `nice_to_have: true` for
   capabilities you'd like to track without gating CI.
4. Add a handful of `forbidden_relationships` — known false-positive
   shapes the extractor must NOT emit.
5. Re-run `build/grafel quality internal/quality/golden/<name>` until
   it exits 0, OR file an issue against the indexer with the recall miss.

## Authoring tips

- **Entity matching** is by `(kind, name, source_file)`. The fixture
  doesn't need full SHA-truncated IDs — those are computed by the indexer.
- **Relationship matching** resolves both endpoints by name+kind, then
  looks up the `(from_id, to_id, kind)` triple. Bare-name targets (e.g.
  `ext:django`, `scope:component:...`) are supported via `to_bare_name`.
- Entities are emitted under multiple kinds simultaneously (e.g. the
  Django `User` class is BOTH a `SCOPE.Component` and a `Model`). Pick
  the kind that owns the edge you care about — typically `Model` /
  `View` / `Route` for framework edges, `SCOPE.Component` for plain code.
- The reporter annotates each missing edge with WHY (neither endpoint
  extracted / from missing / to missing / both present but no edge). The
  last category is the interesting one for indexer work.

## How this fits with bug-rate

Both surfaces are reported, both can block a release:

```
verify2 (bug-rate)         — classification correctness of EXTRACTED edges
quality (this framework)   — completeness vs hand-curated expectations
```

A regression in either is a regression. Bug-rate is corpus-scale (134+ PRs
have driven it from 30%+ down to ~11%); quality runs are tiny by design
(<10 files per fixture) so a fixture failure points at a specific code
path rather than a corpus-level trend.

## Phase 1 scope (PR #600)

- Framework + `grafel quality` subcommand
- One fixture: `python-django-mini`
- Unit tests for the matcher

## Phase 2 scope (PRs #617 + #607)

- Four new fixtures: `typescript-react-mini`, `java-spring-mini`,
  `go-chi-mini`, `rust-tokio-mini`
- ADR-0016 compat fix in `runQuality` (`WithExportJSON` so the harness
  produces `graph.json` alongside `graph.fb`)
- CI wiring: `.github/workflows/quality.yml` + `scripts/verify2/run-quality.sh`
  make quality a per-PR gate; per-fixture JSON artifacts are uploaded on every run
- All five fixtures achieve 100% must-have recall + 0 forbidden hits on `main`

> **Both bullets above have since gone stale — they describe PR #607, not `main`.**
> `quality.yml` is `workflow_dispatch` only (as is every workflow in this repo),
> so quality is not a per-PR gate; and the fixture set grew from 5 to 20 without
> the new fifteen ever reaching 100%. See "Where the gate actually stands" below.

## Where the gate actually stands (Refs #6231)

Measured on `72c7848ca` (2026-08-08) with `scripts/quality/run.sh --runs 1`:

| | count |
|---|---|
| fixtures at 100% must-have recall | 6 |
| fixtures below 100% | 12 |
| fixtures with no `expected.json` (never graded) | 2 |
| forbidden-relationship hits, anywhere | 0 |

The twelve shortfalls are not twelve independent gaps. They cluster:

1. **Background-job frameworks not extracted** (`csharp-hangfire-mini` 0/5,
   `csharp-quartz-net-mini` 0/5, `java-quartz-mini` 2/7, `python-dramatiq-mini`
   0/4, `python-rq-mini` 0/3) — job classes are not emitted as `SCOPE.Service`
   and enqueue/schedule sites are not emitted as `SCOPE.Operation`. One
   capability, 22 of the 26 missing must-have entities.
2. **Controller/view class not emitted as a container**, so its `CONTAINS`
   edges dangle (`csharp-aspnet-core-mini`, `kotlin-spring-mini`,
   `python-django-mini`).
3. **`CONTAINS` missing from a class that *was* extracted** (`scala-play-mini`,
   remainder of `python-django-mini`).
4. **`CALLS` into library/external symbols not recorded**
   (`typescript-react-mini` `ext:useState`/`ext:fetch`/`ext:useNavigate`,
   `elixir-phoenix-mini`, `python-django-mini`).
5. **Swift `Package.swift` target dependencies not emitted as `DEPENDS_ON`**
   (`swift-package-mini` 0/7).

The two ungraded fixtures are not an indexing failure: `groovy-grails-mini` and
`swift-swiftui-mini` have a `src/` tree but **no `expected.json`**, so
`grafel quality` exits before it indexes anything. They were added as resolver
test corpora (#1030, #1008) and landed under `golden/` without expectations.

## The ratchet

```bash
scripts/quality/run.sh --runs 1 --ratchet          # enforce the recorded floor
scripts/quality/run.sh --runs 1 --update-baseline  # re-record after a change
```

`internal/quality/golden/baseline.json` records per-fixture
`entity_found/expected` and `relationship_found/expected`. `--ratchet` fails when:

- a fixture's recall **drops** below the recorded figure (a regression);
- a fixture's recall **rises** above it (unrecorded progress — record it so the
  new, higher figure becomes the floor);
- a fixture's declared expectations change without the baseline being re-recorded;
- any forbidden-relationship hit appears;
- a fixture directory has no baseline entry, or vice versa;
- an `expected.json` appears or disappears without the baseline agreeing.

That last set is also enforced cheaply, without indexing anything, by
`go test ./internal/quality -run TestBaseline` — so a new fixture cannot be
added and quietly escape the gate, and a red fixture cannot be made green by
deleting its `expected.json`.

Running the strict gate (no flags) still demands 100% on every fixture. It is
kept, and kept failing, so the real gap stays visible rather than being defined
away.
