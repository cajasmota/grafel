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
> `quality.yml` is `workflow_dispatch` only, so quality is not a per-PR gate; and
> the fixture set grew from 5 to 20 without the new fifteen ever reaching 100%.
> (Dispatch-only is specific to this workflow, not a repo-wide convention —
> `cross-platform-compile.yml` and `windows-installers.yml` are deliberately
> always-on, and their headers say so.) See "Where the gate actually stands".

## Where the gate actually stands (Refs #6231)

Measured on `316ecada6` (2026-08-08) with `scripts/quality/run.sh --runs 1`,
after #6273 gave the last two directories expectations:

| | count |
|---|---|
| fixture directories under `golden/` | 20 |
| fixtures at 100% must-have recall | 9 |
| fixtures below 100% | 11 |
| fixtures with no `expected.json` (never graded) | 0 |
| forbidden-relationship hits, anywhere | 0 |

Before #6273, `groovy-grails-mini` and `swift-swiftui-mini` produced no report
and nothing graded them, so every figure quoted from this benchmark — including
those written into #6231 and #6260 — had a denominator of **18** while naming
20. A never-graded fixture is indistinguishable from a passing one at every
surface that reads the benchmark.

**Both denominators, so the correction can be read honestly:**

| | entities | relationships |
|---|---|---|
| the 18 that were actually gated | 252/258 = **97.7%** | 86/121 = **71.1%** |
| all 20 | 300/316 = **94.9%** | 138/182 = **75.8%** |
| the 2 newly gated | 48/58 = 82.8% | 52/61 = 85.2% |

The correction moves entity recall **down** (97.7% → 94.9%) and relationship
recall **up** (71.1% → 75.8%). Neither direction should be read as a result:
the two newest fixtures are the only ones whose expectations were written by
someone who could already see what the extractors emit, and where the bar sits
is therefore a choice, not a measurement. Both were written to a rule stated in
the fixtures themselves (`selection_rule`) — gate every member declared in the
source, at the kind and name the rest of the corpus already uses — which is the
strictest reading available and puts them below the corpus entity average
rather than above it. An earlier draft of the same two fixtures, using a more
forgiving rule, scored 97.1% / 78.4%; that number is not quoted anywhere
because it was the softer bar, not a different measurement.

The eleven shortfalls, by fixture — 16 missing must-have entities and 44
missing must-have relationships in total:

| fixture | entities | relationships |
|---|---|---|
| `csharp-aspnet-core-mini` | 4/5 | 0/0 |
| `elixir-phoenix-mini` | 22/22 | 9/10 |
| `groovy-grails-mini` | 12/16 | 14/18 |
| `java-quartz-mini` | 6/7 | 0/0 |
| `java-spring-mini` | 25/25 | 15/21 |
| `kotlin-spring-mini` | 18/19 | 7/12 |
| `python-django-mini` | 25/28 | 5/12 |
| `scala-play-mini` | 23/23 | 5/10 |
| `swift-package-mini` | 6/6 | 0/7 |
| `swift-swiftui-mini` | 36/42 | 38/43 |
| `typescript-react-mini` | 21/21 | 13/17 |

They cluster:

1. **Members declared on a type but not emitted at all** — the largest entity
   cluster, 6 of the 16. `swift-swiftui-mini`: the nested `enum User.CodingKeys`,
   the computed `body` on both Views, `UserListViewModel.init`, and the computed
   `UserService.usersPublisher`. `groovy-grails-mini`: the injected
   `PostController.postService`. Each also drags its `CONTAINS` edge down.
2. **Class emitted under the wrong kind, or a member under the wrong name** —
   `groovy-grails-mini`, where the GORM domain `Post` is `SCOPE.Schema` rather
   than `SCOPE.Component` and its fields are named bare (`title`) rather than
   qualified (`Post.title`) as every other language in the corpus names them.
   Same shape as the `known_regressions` entries filed under #6275/#6276, where
   a kind change reads as a miss because `internal/quality/diff.go` matches
   exactly on `Kind\x00Name`.
3. **Controller/view class not emitted as an entity at all** —
   `csharp-aspnet-core-mini` (`UsersController`, its only miss, and the fixture
   declares no relationships), `kotlin-spring-mini`, `python-django-mini`
   (where the missing class additionally drags its `CONTAINS` edges with it).
4. **`CONTAINS` missing from a class that *was* extracted** — `scala-play-mini`,
   remainder of `python-django-mini`.
5. **`CALLS` into library/external symbols not recorded** —
   `typescript-react-mini` (`ext:useState`/`ext:fetch`/`ext:useNavigate`),
   `elixir-phoenix-mini`, `python-django-mini`.
6. **Swift `Package.swift` target dependencies not emitted as `DEPENDS_ON`** —
   `swift-package-mini` 0/7, its only miss.
7. **HTTP endpoint recorded with a truncated path** — `swift-swiftui-mini`
   emits `http:POST:/` for a request built with
   `baseURL.appendingPathComponent("users")`.

The background-job cluster that dominated this list — `csharp-hangfire-mini`
0/5, `csharp-quartz-net-mini` 0/5, `python-dramatiq-mini` 0/4, `python-rq-mini`
0/3 — is **gone**: #6260 turned the custom extractors on for the benchmark and
all four now score 100%. `java-quartz-mini` went 2/7 to 6/7. This paragraph
replaces a cluster list that still quoted those zeroes long after they were
fixed, and asserted "22 of the 26 missing must-have entities" against a table
that no longer supported it.

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
- a fixture directory has no `expected.json` at all, or the baseline records one
  as `expectations_missing` (#6273 — that flag used to be an accepted answer,
  which is how two directories stayed ungraded across every re-record).

A report is only graded if it carries the current run's `run_stamp`. The
reports directory is never cleared, and the default (`reports/quality`) is
gitignored and long-lived, so without this a run that measured nothing —
a broken `GRAFEL_BIN`, say — would grade the previous run's JSON and report OK.

That last set is also enforced cheaply, without indexing anything, by
`go test ./internal/quality -run 'TestBaseline|TestGoldenSetIsFullyGraded'` —
so a new fixture cannot be added and quietly escape the gate, and a red fixture
cannot be made green by deleting its `expected.json`, nor demoted to ungraded by
deleting it and flipping the baseline entry in the same commit.
`TestGoldenSetIsFullyGraded_6273` asserts three absolute numbers — directory
count, every directory carrying `expected.json`, and the count the baseline
gates — because a test that only asserts `ratchet check` exits 0 is vacuous:
`--update-baseline` re-records whatever it sees and `--ratchet` then agrees.

### What the ratchet does *not* do

- **It is review-gated, not self-defending.** `--update-baseline` will happily
  record a regression as the new floor. Nothing stops that but a reviewer
  reading the `baseline.json` diff, which is why the numbers are one per line.
  That is a normal property of a ratchet; it is not a tamper-proof gate.
- **It compares counts, not identities.** If an extractor stops emitting
  must-have A and starts emitting must-have B, the count holds and the ratchet
  passes. Every report already carries `missing_entities` and
  `missing_relationships`; the gate does not yet consult them.

Running the strict gate (no flags) still demands 100% on every fixture. It is
kept, and kept failing, so the real gap stays visible rather than being defined
away.
