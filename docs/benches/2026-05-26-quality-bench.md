# archigraph MCP — Quality Benchmark Report

**Group:** `client-fixture-a` (3 repos: `<django-monorepo>`, `<react-typescript-repo>`, `<mobile-app-repo>`)
**Ref:** `develop` · **Indexed SHA:** `d9eb1bb801ba`
**Run ID:** `2026-05-26-215721`
**Generated:** 2026-05-26 by `/archigraph-graph-quality`

---

## Executive verdict

On this 10-question benchmark, **archigraph MCP underperformed grep+read on all three dimensions measured**:

| Dimension | grep+read (Phase 2) | archigraph MCP (Phase 3) | Δ |
|---|---|---|---|
| Quality score (full=1, partial=0.5) | **9.5 / 10 (95 %)** | 7.5 / 10 (75 %) | −20 pts |
| Total tokens consumed | **94 411** | 136 554 | +44.6 % |
| Wall time | **7 min 11 s** | 13 min 52 s | +93 % |
| Self-confidence (avg) | **4.4 / 5** | 4.3 / 5 | −0.1 |
| Tool uses (host count) | 73 | 75 | ≈ tied |

MCP matched grep+read perfectly on single-symbol lookups and call-graph questions (Q01, Q02, Q03, Q04, Q06, Q07). It lost decisively on questions requiring **exhaustive enumeration** (Q08 field reads/writes — missed 8 sites, fabricated one classification) and on the **cross-repo HTTP orphan audit** (Q10), where a graph indexing bug surfaced.

**Recommendation:** Do **not** run `/generate-docs` against this index in its current state. Two indexer bugs (no attribute-access edges, broken cross-repo HTTP resolver) will systematically distort the resulting documentation. Address the bugs in [§ Indexer issues exposed](#indexer-issues-exposed) first, re-index, and re-run this benchmark.

---

## Quality breakdown by question

| # | Category | grep+read | archigraph MCP | Divergence |
|---|---|---|---|---|
| Q01 | Entity lookup (`TokenAuthenticationMiddleware`) | ✅ full | ✅ full | tied |
| Q02 | Reference finding (callers of `validate_id_token`) | ✅ full | ✅ full | tied |
| Q03 | Cross-stack trace (mobile login flow) | ✅ full | ✅ full | tied |
| Q04 | Pattern discovery (mobile service triad) | ✅ full | ✅ full | tied |
| Q05 | Architecture overview (backend subsystems) | ✅ full | ◑ partial | grep cleaner taxonomy |
| Q06 | Subsystem deep-dive (RBAC) | ✅ full | ✅ full | tied |
| Q07 | Specific trace (note + attachment create) | ✅ full | ✅ full (1 minor extra) | tied |
| Q08 | Data access (`User.id_token` read/write sites) | ✅ full | ◑ partial | **grep enumerated 8 more sites; MCP mis-classified one source file** |
| Q09 | HTTP cross-repo (mobile→backend endpoint count) | ✅ full | ◑ partial | MCP reported 25 "orphan_calls" that are a graph artifact |
| Q10 | HTTP cross-repo (orphan backend endpoints) | ◑ partial | ◑ partial | **MCP resolver bug: 473 "orphans" = total definitions; some cited orphans ARE called by frontend** |

**Tallies:**
- grep+read: **9 full / 1 partial / 0 wrong / 0 unknown**
- archigraph MCP: **5 full / 5 partial / 0 wrong / 0 unknown**

---

## Indexer issues exposed

Each of these surfaced because the benchmark exercised structural questions MCP is meant to be good at, and MCP fell short of grep+read.

### 1. No attribute-access (field read/write) edges

**Symptom:** Q08 asked "where is `User.id_token` read or written?" grep enumerated 12 reads + 8 writes across 8 files; MCP enumerated only 5 sites and explicitly noted "MCP has no first-class field-access edge in this graph". MCP additionally mis-classified a source file as updating field attributes when it actually assigned the field and called `save()`.

**Impact on docgen:** Any "Data Model" section that wants to explain who touches a field becomes guess-work. Most refactor-impact questions ("if I drop column X, who breaks?") cannot be answered structurally.

**Fix:** Add an `ATTRIBUTE_ACCESS` edge type (model field → operation) during indexing. Both ORM `.filter(field=...)`/`.get(field=...)` calls and `obj.field = ...` assignments are statically detectable.

### 2. Cross-repo HTTP resolver is severely under-matching

**Symptom:** Q10 asked for orphan backend endpoints. `archigraph_endpoints(orphan_only=true)` returned **473** items — identical to the total definition count for the repo. Meanwhile per-repo stats show only **20** of the mobile app's 44 HTTP calls resolve cross-repo (others land in `orphan_calls`), and the frontend has only **393** cross_repo_links against 473 definitions. Concretely, MCP's cited "orphans" include endpoints like `GET /api/v1/notifications` and `GET /api/v1/print-letter`, which the frontend clearly calls.

**Probable cause:** The mobile/frontend clients call paths like `/notifications/...` against an axios `baseURL` that already contains `/api/v1`. Server-side definitions are registered as `/api/v1/notifications/...`. The graph resolver appears to compare these literally, so the `/api/v1/` prefix on definitions never matches the client paths — every call falls into `orphan_calls` and every definition reports as having zero callers.

**Impact on docgen:** API surface inventories, "who calls this endpoint" sections, and any cross-stack architecture diagrams will be wrong by ~85 %.

**Fix:** Normalize the prefix during link resolution. Two reasonable approaches: (a) strip a configurable set of base prefixes from definitions before matching (`/api/v1`, `/api/v2`); (b) prefer suffix-matching when both candidate and definition end in the same parameterized segment.

### 3. `archigraph_find` BM25 ranking is noisy

**Symptom:** Phase 3 agent: "`archigraph_find` BM25 ranking is noisy (Migration, try_catch patterns dilute results); `search_entities` is more precise for substring lookups." This matches the pre-existing finding in an earlier audit.

**Impact:** Forces multi-call exploration where one good search would do, contributing to MCP's higher tool-call count and wall time.

**Fix:** Down-weight or kind-filter `Migration`, `error_handling:try_catch:*`, and other structural-noise entity classes when scoring.

### 4. Per-call MCP latency dominates on cross-stack traces

**Symptom:** Each MCP call took ~7-9s wall time. Cross-stack traces (Q03, Q07) require 5-7 chained calls, pushing them into the 45-60s range each, whereas grep+read finished them in under 30s.

**Fix:** Outside the scope of indexing — but worth profiling the daemon's slow path.

---

## What MCP did do well

To be balanced about it:

- **Single-symbol entity lookup** (Q01): one-shot `archigraph_find` → `archigraph_get_source` returned exact entity + full source, no grep needed.
- **Call-graph navigation** (Q02): `archigraph_find_callers` returned an authoritative caller list at depth=1 — equivalent to grep + manual file inspection but with cleaner provenance.
- **Convention/doc retrieval** (Q04): documentation file is indexed as an entity, so `archigraph_get_source` on it returned the entire document verbatim. This was genuinely faster than grep+Read because no string matching was needed.
- **Architecture overview surface area** (Q05): `archigraph_clusters` returned all 34 Louvain communities with sizes and top entities in one call. The *taxonomy* was less interpretable than human-named buckets, but the quantitative shape was useful.

The pattern: MCP is competitive on **point-lookup and 1-hop graph navigation**. It underperforms grep when the question requires (a) field-level dataflow, (b) cross-repo edge resolution, or (c) exhaustive enumeration.

---

## Cost projection

If a documentation session were to ask 1 000 questions of similar mix:

| Cost dimension | grep+read | archigraph MCP | Extra MCP cost |
|---|---|---|---|
| Input tokens | ~9.4 M | ~13.7 M | +4.3 M |
| Wall time | ~12 h | ~23 h | +11 h |
| Dollar cost (Sonnet 4.6 @ $3/M input) | ~$28 | ~$41 | +$13 |

The dollar delta is small; the wall-time delta and the quality regression are the real costs.

---

## Recommendations to the archigraph coordinator

In order of impact:

1. **Fix the cross-repo HTTP resolver** (issue #2 above). This is the single biggest unlock — it converts MCP from "unreliable on cross-stack questions" to "structurally accurate on cross-stack questions", which is one of the things grep genuinely cannot do well.

2. **Add `ATTRIBUTE_ACCESS` edges for model fields** (issue #1). Without this, the graph cannot answer the most common refactor-safety question — "what breaks if I change this field?".

3. **Down-weight structural noise in `archigraph_find` ranking** (issue #3). Cheap to implement; immediate UX improvement.

4. **Do not run `/generate-docs` until #1 and #2 ship.** A docgen pipeline that consumes the current graph will produce confidently-wrong API inventories and incomplete data-model sections, which is worse than producing no docs at all.

5. **Re-run this benchmark after the fixes ship**, with `--baseline` pointed at this report, to verify the regressions clear.

---

## Methodology

This benchmark followed the `/archigraph-graph-quality` skill's six-phase pipeline. Phases 2 and 3 were run in **fully isolated subagent contexts** — the grep-only run completed before the MCP-only subagent was spawned, and the MCP subagent was prohibited from reading the grep run's output. The Phase 4 judge established ground truth via its own independent grep+read+MCP cross-check pass before opening either answer file.

- **Phase 1** (questions): grounded in entities discovered via `archigraph_clusters`/`stats`/`find`. 10 questions across the 9 mandated categories; auth-heavy because that's what the investigation had just been focusing on (Q01, Q02, Q03 lean here), with the rest spanning architecture (Q05), RBAC (Q06), mobile patterns (Q04), data-access (Q08), and cross-repo HTTP (Q09, Q10).
- **Phase 2** (grep-only): tools restricted to `Bash`/`Read`. 73 tool uses, 94 411 tokens, 7m 11s.
- **Phase 3** (MCP-only): tools restricted to `mcp__archigraph__*` + reading `questions.json`. 75 tool uses, 136 554 tokens, 13m 52s.
- **Phase 4** (judge): independent ground truth then scored both runs. 73 tool uses, 127 756 tokens, 7m 13s.

Token counts come from the host's `usage_info` per subagent (total_tokens reported on each Agent call), which captures input + output + cache reads for the entire subagent session.

---

## Raw-data appendix

### Phase 2 answers (grep+read)

See the run directory `without-mcp.json`.

### Phase 3 answers (archigraph MCP)

See the run directory `with-mcp.json`.

### Phase 4 judgments

See the run directory `judgment.json`.

### Subagent telemetry

| Phase | Tokens | Tool uses | Wall ms |
|---|---:|---:|---:|
| 2 (grep+read) | 94 411 | 73 | 431 270 |
| 3 (archigraph MCP) | 136 554 | 75 | 832 274 |
| 4 (judge) | 127 756 | 73 | 433 606 |
| 6 (calibration) | 81 681 | 73 | 700 643 |

---

## Extraction calibration (Phase 6)

A separate audit of the graph itself — independent of the benchmark questions — to quantify whether the index is the right size. Both grep+Read on the source tree and archigraph MCP queries were used; ground truth came from the on-disk repo.

**Verdict:** **over-biased.** The index has substantial duplication AND a critical relationship gap. Noisy entities, missing edges.

Of **19 409** total entities, the audit identified **~3 200 redundant nodes** (~16 % of the corpus) that should be merged or converted to edges, and **~95 %** of cross-repo HTTP calls are unlinked due to a single resolver bug.

### Over-extraction

| Issue | Count | Rate | Example |
|---|---:|---|---|
| ForeignKey targets mis-emitted as `Constraint` nodes (should be edges) | **~589** | `User`=281 nodes for 1 model; popular models 30-280× each | `User` has 281 entities: 1 legit `Model` + 280 `Constraint` shadows |
| File-level double-emit (`File` + `SCOPE.Component` per source file) | **1 088** | ~1.0× duplicate per source file | source file appears twice in the graph with different IDs |
| Statement-level `error_handling:try_catch:<line>` patterns | **1 077** | 5.5 % of all entities | pattern entities for each try-block |
| ORM-model double-emit (`Model` + `SCOPE.Component scope:ormmodel:*`) | **56** | 100 % of Django models | Each model exists twice with different kinds |
| Migration scaffolding indexed as full Component classes | **100** | 2.3× per migration file (43 files) | 99 nearly identical `Migration` entities — auto-generated |
| Documentation heading/code-block inflation from markdown | **240+** | 240 Heading + 28 CodeBlock + 9 Document | markdown structure entities |
| Class-level dual representation (class + Module + file Component) | sampled 20 % | small fraction but compounds | single class represented three times |

### Under-extraction

| Issue | Count | Rate | Severity |
|---|---:|---|---|
| **Cross-repo HTTP linker broken — `/api/v1/` prefix not normalized** | **370** orphans of 390 calls | 5.1 % overall · **frontend 0/331 = 0 %** linked · mobile 20/44 = 45 % | 🔴 critical |
| No attribute-access / ORM-field entities or edges | **0** field-access edges | field has 60+ grep hits, 0 graph entities | 🟠 high |
| File-level entities have empty `qualified_name` | **1 088** | 100 % of file-component duplicates carry `qname=''` | 🟡 medium |
| Mobile suffix-only paths not resolved even where backend route exists | **25** of 44 | 57 % mobile-orphan rate | 🟠 high |
| WebSocket / Channels routes have no client linkage | n/a | consumer has 0 inbound cross-repo edges | 🟡 medium |

### Kind distribution snapshot

- `Model` — ~56 canonical Django models
- `Constraint` — **≥500** (most should be edges, not nodes)
- `SCOPE.Component` — largest bucket; contains class duplicates, file-path duplicates, scope duplicates, Migration classes
- `SCOPE.Pattern` — **1 077** (all `error_handling:try_catch:<line>`)
- `SCOPE.Heading` — 240+ (markdown headings)
- `SCOPE.CodeBlock` — 28+ (markdown code fences)
- `File` — 1 088 (paired duplicates of `SCOPE.Component` path entities)

### Prune recommendations (over-extraction)

In rough order of impact:

1. **Convert FK-target `Constraint` nodes into edges.** `User`=281, `Building`=30+, `Group`=many. Estimated removal: 500-800 redundant nodes. Side benefit: graph neighbor queries would return real callers instead of being polluted by 280 identical constraint shadows.
2. **Collapse `File` + `SCOPE.Component` file-path duplicates** into a single canonical `File` entity. Removes ~1 088 nodes and gives every file a meaningful `qualified_name`.
3. **Drop or normalize `error_handling:try_catch:<line>` patterns.** Either remove them entirely (-1 077 nodes) or rename them to point at the enclosing function so they're addressable.
4. **Drop the `scope:ormmodel` `SCOPE.Component` shadow** when a `kind=Model` already exists. -56 nodes.
5. **Reduce Migration extraction** to one summary entity per Django app, not one Component per file. -95 of 100 Migration nodes.
6. **Exclude markdown headings/codeblocks** from extraction by default (or behind an opt-in). -280 noise nodes.
7. **Merge `Module` + `SCOPE.Component(file)` + class-level Component** for files that contain a single top-level class so `archigraph_find()` returns one entity, not three.

### Add recommendations (under-extraction)

In rough order of impact:

1. **🔴 Implement `/api/v1/` prefix normalization in the cross-repo HTTP linker.** This single fix would lift cross-repo resolution from 5 % to an estimated 60-80 %, unlocking the entire 331-call frontend orphan set and most of the 25-call mobile orphan tail. Every "who calls this endpoint" and "what does the frontend talk to" question depends on this.
2. **Add `FIELD_READ` / `FIELD_WRITE` edges for Django Model fields.** Without this, the most common refactor-safety question ("if I drop this column, who breaks?") is structurally unanswerable.
3. **Add suffix/fuzzy matching for HTTP paths** as a fallback after prefix normalization — handles routing-prefix drift between repos.
4. **Index websocket consumers as `ws:*` endpoint entities** with topic-style paths so realtime flows show up in traces.
5. **Backfill `qualified_name` on file-level entities** so they can be looked up by qname, not just id.
6. **Add explicit `TESTS` edges** from test entities to the entities they exercise.

### Bottom-line read

The graph is approximately **16 % over-extracted** (~3 200 redundant nodes that should be merged or converted to edges) and **~95 % under-linked** at the cross-repo HTTP layer. The two issues compound: docgen consuming this index would produce inflated "components" tables, missing API surface coverage, and incomplete data-model docs. Fix the HTTP linker prefix bug first — it's a one-line change in the resolver with the highest leverage on output quality.
