# Engine passes — agent guide

Cross-cutting and framework-specific passes that synthesise higher-order edges (HTTP endpoints, ORM queries, message topics, process flows) from raw extractor output.

## Conventions

- **Pass naming** is `<framework>_<concern>.go` — for example `phoenix_routes_test.go`, `http_endpoint_csharp_client.go`, `django_model_cross_refs_test.go`. Stay consistent so passes group sensibly in `ls`.
- **HTTP endpoint resolution is centralised** in `http_endpoint_resolve.go` + `http_endpoint_synthesis.go`. Per-framework passes emit candidate endpoints; the resolver merges, normalises paths (`http_path_normalize.go`), and deduplicates. Do not emit final `http_endpoint` entities from a per-framework pass — feed candidates to the resolver.
- **Pass signatures are guarded** by `pass_signature_guard_test.go` — keep pass entry points stable.
- **Declarative framework support** lives under `internal/engine/rules/*.yaml`. Prefer a YAML rule pack over Go code when the framework can be described declaratively (routes, decorators, naming patterns). Reach for Go only for behaviour the rule schema cannot express.

## YAML rule packs

- One directory per framework / concern under `internal/engine/rules/<name>/`.
- Validated by `internal/engine/rules/_engine/` at load time — broken YAML fails fast.
- Rule packs are the preferred extension point for new frameworks. Adding a pack does NOT require touching Go in this directory if the existing `_engine` runner handles your patterns.

## Process flows + workflows

- DAG construction: `process_flow_dag.go`
- Workflow edge synthesis (Temporal / Cadence / Step Functions): `workflow_edges.go`
- Broker entries (Kafka / SQS / Pub-Sub): see `process_flow_broker_entry_test.go` for invariants.

## Coverage matrix update

When a new framework synthesizer lands — or an existing one materially changes what it emits — update `docs/coverage/registry.json` in the same PR. See the root `AGENTS.md` for the workflow. Typical engine PRs that trigger an update:

- New framework pass (Phoenix routes, Rails controllers, ASP.NET attribute routing, etc.) → add or update the `lang.<lang>.framework.<name>` record with `handler_attribution` / `route_synthesis` cells filled in
- New broker / topic pass → update the relevant `runtime.messaging.<name>` record
- HTTP resolver improvements that change cross-language linking → may require updating cites on multiple framework records

## Related

- Per-language extractors that feed these passes: `internal/extractors/AGENTS.md`
- Cross-repo linker (consumes synthesised endpoints): `internal/links/AGENTS.md`
