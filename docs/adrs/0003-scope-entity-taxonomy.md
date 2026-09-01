# ADR-003: SCOPE entity taxonomy as the data model

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

A code knowledge graph that wants to answer questions like "which queue does this lambda consume?" or "what controllers route to this view?" needs a node vocabulary rich enough to distinguish three layers:

1. **Runtime concepts** — functions, classes, modules, variables; the things a language parser produces.
2. **Framework concepts** — controllers, routes, queues, lambdas, hooks, JSX components, viewsets; concepts that exist as patterns within frameworks (Django, Rails, Express, Next.js, NestJS, FastAPI, etc.) but are not first-class in the host language's AST.
3. **Infrastructure concepts** — deployed resources defined by IaC (queues, topics, tables, buckets, deployments) that bridge to runtime concepts via configuration.

A flat node typology with a single string `type` field collapses these layers, loses framework semantics, and forces every query to special-case identifier patterns. Per-language ad-hoc types fragment further, since a "controller" looks different across stacks but is the same conceptual node for the agent's purposes.

We need a single, namespaced typology that all extractors emit into and that all queries can rely on. The typology must be stable enough to commit to in v1.0 but small enough to fit in agent context.

## Decision

grafel adopts the SCOPE entity-kind hierarchy as its canonical node taxonomy. Internal node types are namespaced under `SCOPE.*`:

- **Code structure**: `SCOPE.Operation`, `SCOPE.Component`, `SCOPE.Schema`, `SCOPE.Variable`, `SCOPE.Reference`
- **Behavioral patterns**: `SCOPE.Pattern`, `SCOPE.Evolution`
- **Web / framework**: `SCOPE.Endpoint`, `SCOPE.Route`, `SCOPE.Service`, `SCOPE.View`, `SCOPE.UIComponent`, `SCOPE.JSX`, `SCOPE.Stylesheet`
- **Async / data**: `SCOPE.Queue`, `SCOPE.Event`, `SCOPE.Datastore`, `SCOPE.DataAccess`, `SCOPE.ExternalEndpoint`
- **Infrastructure**: `SCOPE.InfraResource`

Edges use a fixed vocabulary of relationship kinds: `CALLS`, `IMPORTS`, `DEPENDS_ON`, `IMPLEMENTS`, `EXTENDS`, `USES_HOOK`, `ROUTES_TO`, `SUBSCRIBES_TO`, `TRIGGERS`, `ACCESSES_TABLE`, plus the rest of the relations enumerated in `AllRelationshipKinds()` (`internal/types/kinds.go`), which is the authoritative list. See the amendment below — the original wording of this paragraph named four kinds that were never implemented.

The MCP rendering layer **strips** the `SCOPE.` prefix when surfacing entities to the agent: agents see `Operation`, `Component`, `Endpoint` etc., not `SCOPE.Operation`. The internal storage and on-disk JSON keep the namespaced form so the typology remains explicit in the data model and so future namespaces (e.g., `META.*`) can coexist without collision.

## Consequences

### Positive
- One vocabulary across all languages and frameworks; queries do not branch on stack.
- Framework semantics are preserved: a Django viewset and an Express controller both become `SCOPE.Component` with framework-specific tags, queryable uniformly.
- The infrastructure layer participates as a peer rather than as an afterthought, enabling cross-layer questions (which lambda consumes which queue).
- Stripping the prefix at the MCP boundary keeps agent context short and human-readable.

### Negative
- Extractors must agree on where each language construct lands (e.g., a Rust trait → `SCOPE.Schema`? `SCOPE.Pattern`?). The mapping decisions are documented per-language in `EXTRACTORS.md`.
- The closed relationship enum requires schema-versioning discipline; adding a new edge kind is a breaking change for downstream consumers.
- Agents calling the MCP and authors reading the on-disk JSON see slightly different vocabularies (prefix vs no prefix); the rendering rule must be documented.

### Neutral
- The taxonomy is opinionated; teams with very different mental models may want to remap, which is supported via configuration but not encouraged.

## Alternatives considered

- **Ad-hoc per-language types** — rejected: forces every consumer to learn N typologies and special-case every query.
- **Single-string `type` field with free-form values** — rejected: loses framework semantics, makes filtering brittle, and prevents schema validation.
- **Reuse an external ontology (e.g., UML, schema.org)** — rejected: external ontologies do not cleanly express runtime/framework/infra distinctions in code.
- **Two separate graphs (code vs infra) joined at query time** — rejected: doubles the storage model and complicates queries that span layers; see ADR-006 for the single in-memory graph decision.

## Amendment (2026-09-02) — the "closed enum" named four edge kinds that were never built (issue #6741)

The Decision section above originally read *"Edges use a **closed enum** of relationship kinds: … `CONSUMES_QUEUE`, `TRIGGERS_LAMBDA`, `READS_TABLE`, `WRITES_TABLE` …"*. Every one of those four is absent from `AllRelationshipKinds()`, and `internal/types/kinds_test.go` has asserted since it was written that all four **must not** be valid, in as many words: *"CONSUMES_QUEUE has no producer; must NOT be valid"*.

The ADR is the side that was wrong, and this amendment corrects it rather than the test. The names were a pre-implementation sketch of the vocabulary, written 2026-05-08 before any of these passes existed. What shipped instead is more general and is what every producer and consumer in the tree actually uses:

| ADR-0003 sketch | What was implemented |
|---|---|
| `CONSUMES_QUEUE` | `SUBSCRIBES_TO` / `DELIVERS_TO` (broker pub-sub), `ENQUEUES` + `TRIGGERS` (background-job queues) |
| `TRIGGERS_LAMBDA` | `TRIGGERS`, plus `EVENTBRIDGE_TRIGGERS` / `EVENTGRID_TRIGGERS` for managed buses |
| `READS_TABLE`, `WRITES_TABLE` | `ACCESSES_TABLE` (with `READS_FROM` / `WRITES_TO` for datastore-level access) |

Declaring the sketch names now, to make the ADR true as written, would be the wrong repair: it would add four kinds with no producer, which is precisely the "documented guarantee nothing emits" defect that #6741 was filed about.

Two further corrections follow from this:

- **The enum is not closed.** It has been added to repeatedly since (`BINDS`, `DEPLOYS`, `MAPS_TO`, `SUPERVISES`, and now `PRODUCES`/`CONSUMES`). The stability commitment that ADR-0003 was reaching for is *append-only* — a kind, once emitted, is not renamed or removed without a migration — not *fixed-size*. The Consequences section's "adding a new edge kind is a breaking change for downstream consumers" should be read as "removing or re-pointing one is".
- **The list in prose is not the source of truth.** `AllRelationshipKinds()` is. This paragraph drifted for four months because nothing read it; `TestADR0003RelationshipKindsAreImplemented` in `internal/types` now parses the enumeration line above and fails if it names a kind `IsValidRelationshipKind` rejects.

`PRODUCES` and `CONSUMES` — declared for the queue hop that ADR-0003 gestured at with `CONSUMES_QUEUE` — and their supplement-vs-replace semantics are settled in **ADR-0028**.
