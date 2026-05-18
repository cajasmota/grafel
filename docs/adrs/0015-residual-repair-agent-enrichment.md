# ADR-0015: Residual repair via agent-side enrichment

- **Status**: Proposed
- **Date**: 2026-05-19
- **Deciders**: Jorge Cajas
- **Related**: ADR-0007 (doc-as-bridge), ADR-0011 (per-language bare-name allowlists), ADR-0013 (cross-file import-aware resolution), issue #543, issue #486 (determinism), issue #15 (enrichment loop)

## Context

archigraph's resolver classifies every cross-symbol reference into one of seven `Disposition` buckets (see `internal/resolve/refs.go:127-194`). Two of those buckets — `DispositionBugExtractor` and `DispositionBugResolver` — are the residual: edges the static analyzer knew enough to *emit* but not enough to *bind*. On the v1.0 ship-gate corpus the residual currently sits at:

- synthetic ship-gate corpus: 12% bug-rate
- `django-realworld`: 7.83%
- `client-fixture` group: 11.34% across three repos (per current ship-gate state)

The strategy used through sprints 14-18 was per-language allowlist expansion: every wave (#494 receiver-type primitive, #533 dynamic URL match, #535 React npm allowlist, #525/#526/#527 in flight) pushed the bug-rate down by chipping at one framework's idioms inside `internal/external/synth.go` (currently 11,333 lines) and `internal/resolve/refs.go` (3,196 lines). That work is genuinely effective for popular, stable stacks (React, Django, Flask, SwiftNIO, etc.) — it is cheap, deterministic, free, offline, and reduces the workload of any downstream pass. It will continue. What it does not cover is the long tail: novel/obscure frameworks the indexer hasn't been taught, dynamic URL construction, runtime/distributed edges, and ambiguities that need 50 lines of semantic context to resolve.

Since #15 we have already run an agent-driven loop for *subjective* enrichment (entity descriptions, domain classification, role descriptions). The mechanics — emit a candidate JSON, let an LLM-equipped agent reason over it, merge back on the next index — are proven in `internal/enrichment/candidates.go`. We have an MCP server (`internal/mcp/server.go`, `internal/mcp/tools.go`) the agent already speaks to. The architectural ingredients to push the residual through the same loop are all in place; they have not been wired up yet.

This ADR adds residual repair as a **first-class indexer phase that complements deterministic static analysis**, not a replacement for it. Indexer continues to be the deterministic default for everything statically visible (and per-language waves continue to land for popular stable stacks); repair is the long-tail closure path for what static analysis structurally cannot reach. Both ship in v1.0.

ADR-0007 already established the doc-as-bridge precedent for dynamic edges; this ADR generalizes that pattern to the structural residual.

## Decision

Residual repair becomes a first-class indexer phase. The system has three new artefacts and one new flow:

1. **`enrichment-candidates.json` v2** — extended with `kind: "repair_edge"` records, one per residual edge. Carries enough context (`from_entity`, `relation`, `original_stub`, `disposition_reason`, `candidates`, `context_window`, `extracted_metadata`) for an off-line agent to make a binding decision without re-reading the repo.
2. **`repair.json` v1** — written by the agent (directly or via MCP). One record per repaired `edge_id`, with `resolution` (bind / reclassify / abandon), `confidence`, `reasoning`, `source: "agent-repair"`.
3. **Repair-apply phase** — runs *before* disposition classification in `cmd/archigraph/index.go` (see the disposition-reclassification pass around lines 321-411). Repairs are validated, applied as overrides, attribution is stamped onto the edge property bag, and a `repair_stats.json` is emitted.
4. **MCP surface** — `list_residuals(repo, limit, cursor)` paginates the v2 candidates filtered to `kind == "repair_edge"`. `submit_repair(...)` validates and appends to `repair.json` atomically.

The architectural rule: **the indexer is the source of truth for structure that is statically visible; the agent is the source of truth for structure that requires per-call-site context.** Repairs are merged in, not blended — every edge carries a `source` property naming who decided it.

### Key data-contract decisions

- **`edge_id` = `sha256(from_id || relation || original_stub)[:16]`** — stable across runs as long as the source line stays put. The `from_id` is itself a content hash (per `internal/graph/`), so renaming a file moves the edge_id; that is the intended staleness signal.
- **Repairs do not mutate `graph.json` directly.** They mutate the resolver's internal endpoint table before disposition classification runs. This keeps the existing graph-emit codepath the single writer of `graph.json` and preserves ADR-0005's pre-bake invariants.
- **`repair.json` lives at `<repo>/.archigraph/repair.json`.** Same directory as `enrichment-candidates.json` / `enrichment-resolutions.json`. Sibling files, sibling schemas.
- **Trust model is allowlist, not blocklist.** The indexer enumerates *which* resolutions are accepted (bind_to_entity, reclassify_as_external, reclassify_as_dynamic, reclassify_as_resolved, abandon) and rejects anything else with a recorded reason. New resolution kinds require an ADR amendment.
- **Determinism extension (issue #486).** The repair-apply phase iterates `repair.json` records in `edge_id` sort order, applies them deterministically, and emits `repair_stats.json` in stable sort order. Same source + same `repair.json` → byte-identical `graph.json`.

## Consequences

### Positive

- v1.0 ships with a bug-rate floor independent of how exotic the user's stack is. Novel/obscure frameworks no longer block a release on a per-language wave.
- Per-language allowlist work for popular stable frameworks continues to land and continues to pay — it always reduces the work the agent has to do for the typical case, keeping the deterministic path fast/free/offline. Repair is additive, not substitutive.
- Cross-repo dynamic edges (#510, #533, #534) and runtime edges (#515) get a viable resolution path that was previously stuck on "we cannot see this from source."
- Source-attribution turns the graph from "trust the binary" to "audit the binary"; every controversial edge has a `reasoning` string the user can read.
- Generalizes ADR-0007: docs were the bridge for dynamic content, the agent loop is the bridge for ambiguous structural content. Same architecture, different surface.

### Negative

- Increases schema surface area: `enrichment-candidates.json` gets a new `kind`, a new sibling file (`repair.json`) appears, and a new stats file (`repair_stats.json`) is emitted. Two new MCP tools (`list_residuals`, `submit_repair`) join the existing surface and must be kept compatible across releases.
- Introduces an LLM-cost line item for the user during the repair pass. The deterministic indexer remains free/offline; the repair pass spends one round-trip per residual edge to the user's chosen agent.
- Adds a new failure mode: a misbehaving agent (or hostile `repair.json`) can degrade the graph. Mitigation: strict allowlisted resolutions, verification step rejects invalid targets, `source: agent-repair` makes every edge auditable, `rm .archigraph/repair.json && archigraph index` reverts to pure-static.
- Bug-rate is no longer a property of "what archigraph the binary knows"; it now depends on whether the user has run an agent loop. We must update README + verify2 docs so users do not see 12% on a fresh index and think the tool is broken.
- Adds a second sibling-file pair to `.archigraph/` — `repair.json` and `repair_stats.json`. Inventory of `.archigraph/` is starting to grow; we should produce a `.archigraph/README.md` describing each file's purpose.
- The agent loop has a latency floor (LLM round-trip per residual). On a 5,000-residual graph this is not interactive. Mitigation lives in Phase 3: centrality-ordered prioritization, batch submit, parallel `list_residuals` cursors.

### Neutral

- Reuses the existing `internal/enrichment/` machinery and its file conventions. No new directory, no new package boundary, no new persistence story.
- The repair-apply phase is structurally identical to the existing `enrichment.ApplyResolutions` flow (`internal/enrichment/candidates.go:463`) — same shape, different payload.

## Data-contract summary

Full schemas live alongside this ADR:

- `docs/specs/enrichment-candidates-v2.schema.json`
- `docs/specs/repair-v1.schema.json`
- `docs/specs/mcp-residual-repair-tools.md`
- `docs/specs/repair-trust-model.md`

### enrichment-candidates v2 — change summary

- Schema version bumps from `1` to `2` (`internal/enrichment/candidates.go:41` — `CandidatesSchemaVersion`).
- New candidate kind: `repair_edge`. Carries `edge_id`, `from_entity`, `relation`, `original_stub`, `disposition_reason`, `candidates[]`, `context_window`, `extracted_metadata`.
- All v1 kinds (`describe_entity`, `classify_domain`, `describe_role`, ...) retain identical on-disk shape. v1 readers see `repair_edge` records and skip-by-kind — backward compatible.

### repair.json v1

- Sibling file to `enrichment-resolutions.json`.
- One record per `edge_id`. Resolutions are an enum of five values: `bind_to_entity`, `reclassify_as_external`, `reclassify_as_dynamic`, `reclassify_as_resolved`, `abandon`.
- `source` is fixed at `"agent-repair"`. Future sources (e.g. `"manual-yaml"`, `"ci-rule"`) would land in a v2 of this schema with its own ADR.

### Indexer integration point

Repair-apply runs *between* the resolver's first pass (`internal/resolve/refs.go`) and the disposition-reclassification pass that lives around `cmd/archigraph/index.go:321`. Pseudocode:

```
endpoints := resolver.FirstPass(doc)
repairs   := repair.Read(absRepo + "/.archigraph/repair.json")
applied,
rejected,
stale     := repair.Apply(endpoints, repairs, doc)   // mutates endpoints
final     := resolve.Reclassify(endpoints, doc)       // unchanged
repair.WriteStats(absRepo, applied, rejected, stale)
```

Source-attribution: every endpoint touched by `repair.Apply` gets a `properties["resolved_by"] = "agent-repair"` and `properties["repair_reasoning"] = <one-sentence>` so the graph emit step preserves them on the final edge.

### MCP surface

- `list_residuals(repo, limit, cursor)` — reads `<repo>/.archigraph/enrichment-candidates.json`, filters `kind == "repair_edge"`, slices by cursor, returns the batch plus a `next_cursor`. Cursor is the last `edge_id` in the batch — stateless, deterministic.
- `submit_repair(edge_id, resolution, target?, module?, confidence, reasoning)` — verifies the proposed repair against `repair-trust-model.md` rules, appends to `repair.json` (or replaces an existing record with the same `edge_id`), returns `{ok, written, rejected_reason?}`.
- `reindex(repo)` — optional, lets the agent verify bug-rate dropped post-batch. Out of scope for Phase 1; reuses the existing index path under the hood.

## Risks

1. **Hostile or buggy agent corrupting the graph.** Mitigation: allowlisted resolutions, verification step rejects (a) `bind_to_entity` targets that do not exist in the current document, (b) repairs that introduce self-loops, (c) repairs that contradict an existing `CONTAINS` hierarchy, (d) repairs whose `module` is not a syntactically valid identifier (no path traversal). Every rejection is logged with a stable reason code.
2. **Repair staleness when code outruns the agent.** Mitigation: `edge_id` is content-hash-bound; when source moves, `edge_id` changes; stale repairs fall off naturally and are listed in `repair_stats.json` so the agent knows to redo them.
3. **Agent throughput on large repos.** A 5k-residual graph is several thousand LLM round-trips. Mitigation lives in Phase 3 (centrality ordering, batch submit). Phase 1 ships the synchronous loop and accepts the latency.
4. **Two complementary residual closures.** The per-language allowlist work in `internal/external/synth.go` keeps closing things over time for popular stable stacks. If both lanes run, the indexer wins (it runs first, deterministically); the repair lane just sees a smaller residual. This is the intended both-and steady state, not a competition — the indexer keeps owning the typical case and the repair lane handles what static analysis structurally can't reach.
5. **`repair.json` grows unboundedly.** On long-lived repos with churn, stale records accumulate. Mitigation: `repair_stats.json` reports stale count; a future `archigraph repair gc` command (out of scope for Phase 1) prunes by configurable retention. **OPEN QUESTION:** should stale repairs be auto-pruned on apply, or kept as audit history?
6. **Conflicting repairs for the same `edge_id`.** Two agents writing concurrently. Mitigation: `submit_repair` writes atomically (write-temp-then-rename) and is last-writer-wins by `resolved_at` timestamp. **OPEN QUESTION:** is last-writer-wins acceptable, or do we want optimistic concurrency via a `previous_resolved_at` parameter?

## Migration

- **New repos:** no migration. First `archigraph index` emits v2 candidates; users opt in to repair by running the agent loop.
- **Existing repos with v1 `enrichment-candidates.json`:** v2 reader is backward compatible (v1 has no `repair_edge` kind, just nothing to apply). On next index the file is rewritten in v2 shape; v1 consumers reading the rewritten file see `repair_edge` rows and skip-by-kind.
- **Existing `.archigraph/` contents:** unchanged. `repair.json` is a new file; absence means "no repairs," which is the pre-ADR behaviour.
- **Reverting:** `rm .archigraph/repair.json && archigraph index` returns the graph to pure-static. No data loss outside the repair layer itself.
- **CI / determinism:** the indexer must read `repair.json` at a deterministic point. CI runs that don't want agent influence simply omit the file (or set `ARCHIGRAPH_DISABLE_REPAIR=1` — **OPEN QUESTION:** do we need an env-var kill switch, or is file-presence enough?).
- **Schema upgrades:** version field in both files is required. Indexer rejects unknown schema-version values with a clear error pointing to the relevant ADR.

## Issue impact

This ADR is **both-and**, not a replacement for per-language work. The split below mirrors the corrected #543 umbrella.

### STAY indexer-side (continue normally)

- **#525** — EXTENDS kind-disambiguator: pure resolver fix, cheap, deterministic, halves Python residual.
- **#526** — DRF ViewSet class-attribute extraction: structural, no semantic context needed.
- **#527** — Django URLConf module binder: structural cross-language extractor enhancement.
- **#535** — React npm allowlist (~50 packages): stable popular libs, 100-line PR saves thousands of LLM calls per index.
- **#537** — barrel re-export resolution: pure AST work.
- **#494 for statically-typed languages** (Go, Java, Kotlin, Swift): type info is in the AST, no LLM needed.
- All Phase-1 work and any future per-language wave for **popular stable frameworks** continues on the indexer side.

### BECOME REPAIR-FIRST (indexer emits candidates; agent matches/resolves)

- **#494 for dynamic languages** (Python, Ruby, JS): receiver-type inference needs semantic context; the agent does it better.
- **#510 / #533 / #534** — HTTP route ↔ fetch matching, dynamic URL pattern matching: dynamic URLs are intractable statically, trivial for an LLM.
- **#515** — runtime/distributed edges (Celery task strings, queue names, pub/sub topics): string-based cross-file matching is what LLMs do well.

### UNBLOCKS

- **Novel-framework-of-the-month** problem — agent recognizes any framework by reading code; v1.0 no longer requires a release for new stacks.
- **Cross-repo for stacks without shared imports** (e.g. Django + React + RN over HTTP) — agent provides the cross-repo edges via route/contract reading.

### Remain ORTHOGONAL

- **#486** (determinism) and **#489** (PageRank float drift) — apply equally to both paths.
- **DASH-2 #27** and **DASH-5 #30** — UI/dashboard work, separate channel.
- Embedding/semantic-search v1.1 plan (**#460, #461, #462**) — separate channel from repair.

## Alternatives considered

1. **Continue per-language allowlist waves (status quo).** Rejected: linear cost per framework, never reaches 0% on novel stacks, positions archigraph as a static tool needing constant patches.
2. **Ship a YAML edge-injection file users write by hand.** Rejected for the same reason ADR-0007 rejected manual edge YAML: worse author experience than the agent loop, and we already need the MCP surface for other enrichment.
3. **Embed an LLM in the indexer binary.** Rejected: violates the single-binary-distribution promise (ADR-0001), couples shipping to a model provider, removes user choice of agent.
4. **Defer to v1.1.** Rejected: every released version with a 7-12% bug-rate teaches users the tool is incomplete. The architectural pivot is small enough to land for v1.0 and large enough to redefine the product story.

## Open questions

1. Auto-prune stale repairs on apply, or retain as audit history? (default proposed: retain, emit count, leave GC to a separate command).
2. `submit_repair` concurrency: last-writer-wins, or optimistic concurrency via `previous_resolved_at`? (default proposed: last-writer-wins for Phase 1, revisit if real-world conflicts appear).
3. Env-var kill switch (`ARCHIGRAPH_DISABLE_REPAIR=1`) for CI, or rely on file-presence only? (default proposed: file-presence only — fewer knobs, clearer semantics).
4. Does `reclassify_as_resolved` need to specify the resolver pass it should re-run, or always treat the new target as authoritative? (default proposed: authoritative, no re-run).
5. Should `repair_edge` candidates be emitted for `DispositionExternalUnknown` as well, or only the two bug- dispositions? (default proposed: only bug- dispositions for Phase 1; `external-unknown` is "we know it's external, just not which package" and is a softer failure).
