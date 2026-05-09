# Issue #92 — bug-resolver categorization

**Date:** 2026-05-09
**Corpus:** post-#96 sample-app refresh (apps that USE frameworks, not framework internals)
**Method:** added `internal/resolve.Index.DiagnoseBugResolver` + `cmd/archigraph` env-gated dump (`ARCHIGRAPH_BUG_RESOLVER_SAMPLES=80`); ran per-repo `archigraph index --json-stats`; bucketed via histogram + manual sample inspection.
**Time on harness:** ~3 min for the four repos covered.

forbidden-term grep: clean

## Per-repo baseline

ToID-side bug-resolver edges only (the dump skips FromID — bug-resolver is overwhelmingly a ToID phenomenon, mirroring #91). Histogram TOTAL is therefore lower than `disposition_counts['bug-resolver']`, which counts both endpoints.

| repo | dispositions: bug-resolver | bug-rate% | dump TOTAL | top category |
| --- | ---: | ---: | ---: | --- |
| go-gin | 105 | 43.6 | 72 | `ambig-bare-hint-fail` 52.78% |
| java-spring-petclinic | 84 | 60.1 | 55 | `ambig-bare-no-hint` 52.73% |
| python-flask-realworld | 122 | 52.9 | 113 | `ambig-bare-no-hint` 69.91% |
| ruby-rails-realworld | 39 | 25.6 | 39 | `ambig-bare-no-hint` 100.00% |

**Two categories dominate every repo**: `ambig-bare-no-hint` (bare-name lookup ambiguous; relKind has no registered hint family) and `ambig-bare-hint-fail` (relKind hint registered but the hint family still sees >=2 candidates or zero in the family). `kind-mismatch` and `ambig-kind` are <6% combined across the corpus.

## Diagnostic categories (from `BugResolverDiag.Category`)

The classifier in `classifyDispositionLang` declares an endpoint bug-resolver when the leaf name exists *somewhere* in the graph but `LookupStatusHint` returned a non-rewritten status. `DiagnoseBugResolver` re-walks the same indexes to pinpoint *why*:

| category | meaning |
| --- | --- |
| `kind-mismatch` | stub had `Kind:Name`; that kind exists in the graph but not for this name; bare-name is also ambiguous or absent. |
| `ambig-kind` | `Kind:Name` where `(kind, name)` is itself ambiguous (multiple entities at the same kind+name). |
| `ambig-bare-no-hint` | bare-name lookup ambiguous and the relKind has NO registered hint family in `hintKinds()`. |
| `ambig-bare-hint-fail` | relKind hint exists but the hint family had zero or >=2 candidates with this name. |
| `ambig-qualified` | stub matched a `byQualifiedName` entry but with the blank-string sentinel (collision). |
| `unknown` | none of the above; should be near-zero. |

## Top-3 patterns per repo

### go-gin — bug-resolver=105, dump=72

1. **`DEPENDS_ON RouterGroup` (23 of 72)** — `DEPENDS_ON` has no hint family registered in `hintKinds()`; `RouterGroup` exists multiple times (`Component` + `SCOPE.Component`) so `byName` is ambiguous. Same shape for `SecureJSON`, `PureJSON`, `AsciiJSON` (depended-on response writer types).
2. **`CALLS validate` (16) / `CALLS Default` (9) / `CALLS ResponseWriter` (7)** — `ambig-bare-hint-fail`. The Operation hint family fires but matches multiple entities (e.g. `validate` exists as both Function and Method on different receivers), so `lookupByKindHint` aborts. Receiver-binding gap — same root cause as the cross-file method patterns identified in #91.
3. **`IMPORTS maps` (2)** — `IMPORTS` has no hint family. `maps` collides between the Go stdlib `maps` package (synthesised entity) and a project-internal `maps` Component.

### java-spring-petclinic — bug-resolver=84, dump=55

1. **`CALLS name` from PetControllerTests / ValidatorTests (25 of 55)** — `ambig-bare-hint-fail`. The KindsPresent column reveals the bug: `name` exists ONLY as `Schema` and `SCOPE.Schema` entities (SQL column names from the Petclinic schema files). The Operation hint family has no `name` entity, so the hint returns zero matches. **This is a cross-language false bug-resolver**: a Java `.name(...)` method call collides with a SQL column entity. Should arguably be classified as bug-extractor (no method named `name`) but the `nameExists()` check in `classifyDispositionLang` is kind-agnostic.
2. **`CONTAINS first_name|last_name|name|...` (~12)** — `ambig-bare-no-hint`. SQL schema CONTAINS edges from a `Schema` entity to bare column names. Multiple schemas have a column named `name` / `first_name`. CONTAINS has no hint registered.
3. **`REFERENCES vets|specialties|pets|owners|types|visits` and `INDEXES visits` (~7)** — same SQL-schema ambiguity. `REFERENCES`/`INDEXES` have no hint family.

### python-flask-realworld — bug-resolver=122, dump=113

1. **Project-local Python IMPORTS (`conduit.database.db`, `conduit.user.models.User`, `conduit.exceptions.InvalidUsage`, `..views`, …)** — `ambig-bare-no-hint`. Dotted import paths probed against `byName` against the full string fail when the project's `__init__.py` re-exports the same name from multiple modules (so `User` is ambiguous; the dotted path lookup also misses because `byQualifiedName` keys on the entity's QualifiedName which doesn't include the importing alias style). IMPORTS has no hint family.
2. **External library IMPORTS that DO have a project-internal collision (`marshmallow.Schema`, `marshmallow.fields`, `flask_jwt_extended.jwt_required`, …)** — same `ambig-bare-no-hint` shape, but here the bare leaf (`Schema`, `fields`, `jwt_required`) collides between an external-package alias and an in-project Component. The synth pass should have routed these to `ext:marshmallow` / `ext:flask_jwt_extended` but the importing form (`from marshmallow import Schema`) reaches the resolver as a bare leaf without the package prefix.
3. **CALLS bare-name (8 rows: `Comment`, `Tags`, `User`, `UserProfile`, `_register_user`)** — bare-name calls to constructors and helpers that exist as both `Component` and `Operation` in the graph (model class + factory function with same name); CALLS hint picks Operation family but multiple Operations share the leaf.

### ruby-rails-realworld — bug-resolver=39, dump=39 (100%)

1. **`CONTAINS up|down|change|create|destroy|show|index|update` from controller / migration classes (28 of 39)** — `ambig-bare-no-hint` 100%. The Ruby extractor (`internal/extractors/ruby/ruby.go:95`) emits `ToID: child.Name` — bare leaf — for class→method CONTAINS edges. Every Rails app has dozens of controllers each defining `create`, `destroy`, `index`, etc., so bare-name is always ambiguous. CONTAINS has no hint family. **The fix is at the extractor**: emit a structural-ref `scope:operation:...:<file>:<class>#<method>` instead of the bare method name. (Same pattern applies to ActiveRecord migration `up`/`down`/`change` callbacks — class methods sharing a name across migration files.)
2. **`CONTAINS find_article!` and similar bang/method-name controller helpers (4)** — same shape, bang methods used as `before_action` filters.
3. **`IMPORTS test_helper` (4)** — Ruby `require 'test_helper'` resolves to the project's `test/test_helper.rb`, which exists as a Component. The literal `test_helper` is unique within the corpus but is registered under both `Component` and `SCOPE.Component`, making `byName` ambiguous and `IMPORTS` unable to disambiguate (no hint family).

## Cross-cutting observations

1. **`CONTAINS` is the dominant rel kind for `ambig-bare-no-hint`**. Across rails-realworld, spring-petclinic SQL schemas, and partly java-spring-petclinic, CONTAINS edges from a parent (Class / Schema) to a bare child name are the single biggest pattern. The parent's file IS known at resolve time (FromID is hex by Pass 2.5), so a structural-ref / file-scoped lookup would resolve every one. **Highest-leverage fix.**
2. **No relKind hint for `IMPORTS`, `CONTAINS`, `DEPENDS_ON`, `REFERENCES`, `INDEXES`**. Only `EXTENDS`/`IMPLEMENTS` (Component family) and `CALLS` (Operation family) are wired. Adding hint families for these other kinds would help, but in practice the bare-name pool is too crowded for a kind-only hint to disambiguate (see rails-realworld `create`).
3. **Cross-language same-name collisions inflate bug-resolver**. The most striking case is java-spring-petclinic `CALLS name`: the Java method call collides with a SQL `Schema:name` column entity, and the kind-agnostic `nameExists()` happily reports "exists", flipping the classifier from bug-extractor to bug-resolver. A KindsPresent-aware classifier (when relKind is CALLS and KindsPresent has zero Operation-family kinds, classify as bug-extractor) would re-bucket these.
4. **Project-internal IMPORTS in Python and JS** ride on the dotted-name shape; they need a per-language importer that attempts segment-by-segment resolution against `byQualifiedName` rather than treating the whole dotted string as a single byName key.

## Quick wins evaluated

Considered the following and **did not land any in this PR** for the reasons noted:

- **Add `IMPORTS` / `CONTAINS` / `DEPENDS_ON` to `hintKinds()`**: too coarse to disambiguate the dominant patterns. `create` exists as 6+ Operations in rails-realworld; the Operation-family hint still aborts. Would move <10 edges across the four-repo set.
- **Resolver-side parent-file fallback for CONTAINS**: cleanest fix for the rails-realworld pattern but requires (a) a new `byID → SourceFile` index inside `Index`, (b) a new lookup branch in `LookupStatusHint`, and (c) regression tests. Past the 1–2 hour quick-win envelope and overlaps with the structural-ref work tracked under PORT-2-FIX-2.
- **Promote false bug-resolver to bug-extractor when KindsPresent has no hint-family overlap**: clean two-line classifier patch but bends the issue #44 metric (would re-allocate ~25 of 84 spring-petclinic bug-resolver rows into bug-extractor). That moves the goalposts without fixing anything; better tracked as a separate scoring proposal.

The right shape of fix is per-extractor: emit structural-refs (`scope:operation:<lang>:<file>:<class>#<method>`) instead of bare leaf names for `CONTAINS` edges. The structural-ref resolver path (`lookupStructural`) already handles these without ambiguity. This is per-extractor work, scoped per language, tracked in the follow-up issues below.

## Follow-up issues filed

| # | language(s) | title |
| --- | --- | --- |
| TBD | ruby | Ruby extractor emits bare-name CONTAINS instead of structural-ref (rails-realworld 100% bug-resolver) |
| TBD | java / sql | SQL schema CONTAINS / REFERENCES / INDEXES emit bare column names; Java CALLS to common method names collide |
| TBD | python | Project-internal Python IMPORTS with dotted path don't resolve via byQualifiedName segment matching |
| TBD | go | Go DEPENDS_ON edges to RouterGroup-style framework types ambiguous between Component and SCOPE.Component |
| TBD | resolver | Cross-language bug-resolver mis-classification: kind-agnostic `nameExists()` flips bug-extractor to bug-resolver |
