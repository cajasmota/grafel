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
    └── python-django-mini/
        ├── src/        # Small hand-curated source tree
        └── expected.json
```

## Running

Single fixture:

```bash
build/archigraph quality internal/quality/golden/python-django-mini
build/archigraph quality --json out.json internal/quality/golden/python-django-mini
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
   build/archigraph index --pretty --out /tmp/g.json internal/quality/golden/<name>/src
   jq -r '.entities[] | "\(.kind)\t\(.name)\t\(.source_file)"' /tmp/g.json | sort -u
   jq -r '.relationships[] | "\(.kind)\t\(.from_id)\t\(.to_id)"' /tmp/g.json
   ```
3. Author `expected.json` against the schema in `internal/quality/expected.go`.
   Use `must_exist: true` for the recall floor and `nice_to_have: true` for
   capabilities you'd like to track without gating CI.
4. Add a handful of `forbidden_relationships` — known false-positive
   shapes the extractor must NOT emit.
5. Re-run `build/archigraph quality internal/quality/golden/<name>` until
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

## Phase 1 scope (this PR)

- Framework + `archigraph quality` subcommand
- One fixture: `python-django-mini`
- Unit tests for the matcher
- Baseline: see [baseline.md](./baseline.md)

## Phase 2 (followup issues)

- `typescript-react-mini` — React + hooks + context + useQuery
- `java-spring-mini` — Spring Boot REST controller + JPA repository
- `go-chi-mini` — Go HTTP handler + middleware + struct receiver methods
- `rust-tokio-mini` — Rust async + tokio + traits + generics
- CI wiring (`scripts/verify2/`) so quality is a per-PR gate
