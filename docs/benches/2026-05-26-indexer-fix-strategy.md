# Archigraph Indexer Fix Strategy

**Source benchmark:** `2026-05-26-quality-bench.md` (run id `2026-05-26-215721`)
**Repo audited:** archigraph @ `develop`
**Scope:** Extractor / linker / indexer findings only. Latency, daemon, MCP serialization is being addressed by a parallel agent.
**Out of scope:** anything in `internal/daemon/`, `internal/perf/`, MCP response formatters.

---

## 0. Verdict at a glance

The bench surfaced two structural bugs (HTTP linker, no field-access edges) and four kinds of over-extraction (FK shadows, file double-emit, try/catch pattern nodes, migrations, markdown headings). The HTTP linker fix is by far the highest-leverage change — it converts MCP from "wrong" to "correct" on the entire cross-stack question class. Field-access edges close the only structural-impossibility ("if I drop column X, who breaks?") in the bench.

Prune work is medium-leverage and mostly safe to land in parallel: it shrinks the graph by ~16% (~3 200 nodes of 19 409) and improves `archigraph_find` ranking, but it doesn't change correctness on cross-repo questions.

The parallel agent's GitHub issue inventory revealed **no in-flight PR or open issue that conflicts with this work**. The closest match is an open issue about quality regression testing, but the underlying fixes are not yet filed.

---

## 1. Critical fixes (ship before re-bench)

### Fix A — Cross-repo HTTP resolver under-linking (5.1% → est. 60-80%)

**Bug shape.** Calibration confirms: backend endpoint is registered as a producer; frontend HTTP call is registered as a consumer; they do not link. 0/331 frontend calls, 20/44 mobile calls resolve.

**Code state.** Generic API-prefix stripping exists in two places already:

- `internal/links/http_pass.go:97` `apiPrefixRe = ^/(?:api(?:/v\d+)?|v\d+)(/|$)`
- `internal/links/http_pass.go:128-141` `stripAPIPrefix(...)`
- `internal/links/http_pass.go:376-386` registers each producer hit at BOTH the full and prefix-stripped key in `byPath`.
- `internal/links/http_pass.go:432-436` probes both keys.

The structural resolver at `internal/engine/http_endpoint_match.go` + `http_endpoint_resolve.go` duplicates this for the in-pass call→definition match.

**So why is the link rate still 5%?** Two reachable root causes — both need verification with a minimal repro before patching, but the strategy is the same:

1. **The producer-side synthetic endpoint is NEVER emitted with the right `url_prefix` property** when Django uses nested-include routes. The existing code path emits the synthetic with the composed path but does NOT set `url_prefix`. The URL-prefix-driven strip is therefore dead for these routes. Generic strip should still cover them, BUT —
2. **The probe only fires inside a conditional check.** Consumer-only name buckets DO satisfy this, but some consumer-side aliases may be missing. Audit needed to confirm.

**Where:**
- Primary: `internal/engine/django_urlconf_nested.go` — add `"url_prefix"` property to the synthetic `props` map alongside `verb`/`path`/`framework`/`pattern_type`. This makes the existing path-strip fire deterministically rather than relying on generic inferring it.
- Secondary: `internal/engine/django_routes.go` — same fix for the non-nested code path.
- Verification harness: `internal/links/http_pass.go` test files — add a fixture pair: producer endpoint (with normalized prefix) ↔ consumer endpoint (env-stripped). Assert link emitted.

**What (sketch):**

```go
// django_urlconf_nested.go — extend props with url_prefix when parentPrefix
// is a clean API mount. Use normalised "/api/v1" shape (leading slash, no trailing).
props := map[string]string{
    "verb":         "ANY",
    "path":         canonical,
    "framework":    "django",
    "pattern_type": "urlconf_nested_include",
}
if normalisedPrefix := normaliseAPIMountPrefix(parentPrefix); normalisedPrefix != "" {
    props["url_prefix"] = normalisedPrefix
}
// helper: trim leading/trailing slash, prepend "/", keep only when shape matches
// /api, /api/vN, /vN — the same set stripAPIPrefix handles.
```

Then add an integration test that walks the full real-corpus fixture.

**Effort:** S (half a day). One regex helper + property propagation + 2 tests.

**Risk:** Low. Adding a property to an existing entity is backwards-compatible. The byPath index already handles both prefixed and stripped keys, so the link rate can only go up. Possible false-positive: legitimate non-API routes named `/api/...` (e.g. a literal `/api/docs` page) might over-strip — but the existing `stripAPIPrefix` regex already restricts to `api(/v\d+)?|v\d+` and the test suite covers it.

**Verification:**
1. Rebuild the fixture group index.
2. Check endpoint stats — expect orphan calls to drop significantly, cross_repo_resolved to rise.
3. Re-run `/archigraph-graph-quality --baseline`. Relevant questions should flip from partial → full.

### Fix B — No field-access edges (model field reads/writes)

**Bug shape.** Benchmark asked "where is field X read or written?" MCP returned 0 edges between operation entities and the field. Field ENTITIES do exist, but no `READS_FIELD`/`WRITES_FIELD` edges connect them to the call-sites.

The ORM-queries pass records field access as a *property* on the call synthetic — never as a typed edge. That's invisible to graph neighbor / expand queries.

**Where:**
- New pass: `internal/engine/orm_field_edges.go` (sibling to `orm_queries.go`). Runs after `orm_queries` and after model-field extraction. For every emitted call synthetic with field accesses, look up the target model's field entities and emit `READS_FIELD` / `WRITES_FIELD` edges.
- New extractor pass for direct attribute access: extend the extractor's walkNode logic with an *attribute_access* scanner that detects:
  - `obj.field` reads → `READS_FIELD` edge
  - `obj.field = X` writes → `WRITES_FIELD` edge
  - `Model.objects.update(field=X)` → `WRITES_FIELD`
  - `Q(field=X)`, `F('field')` → `READS_FIELD`

**What (sketch):**

```go
// internal/engine/orm_field_edges.go
package engine

// ApplyORMFieldEdges runs after orm_queries to lift field accesses from a
// property bag onto explicit graph edges.
func ApplyORMFieldEdges(entities []types.EntityRecord, byQName map[string]int) []types.RelationshipRecord {
    var rels []types.RelationshipRecord
    for _, e := range entities {
        if e.Properties["pattern_type"] != ormQueriesPatternType { continue }
        model := e.Properties["model"]      // already populated
        for _, key := range strings.Split(e.Properties["filter_keys"], ",") {
            // field lookups can have suffixes like __exact, __in, __isnull
            base := strings.SplitN(key, "__", 2)[0]
            if base == "" || base == "pk" { continue }
            fieldQName := model + "." + base
            if idx, ok := byQName[fieldQName]; ok {
                rels = append(rels, types.RelationshipRecord{
                    FromID: e.Properties["caller_id"],
                    ToID:   entities[idx].ID,
                    Kind:   "READS_FIELD",
                })
            }
        }
    }
    return rels
}
```

For attribute access, a separate AST pass is needed — it's larger. A reasonable phase-1 is to only emit edges from ORM `.filter()`/`.get()`/`.update()` calls (deterministic, ~80% of field reads come from these). Attribute reads in plain Python requires receiver-type inference and is phase-2.

**Effort:** M (1 day) for ORM-keyword edges; L (multi-day) to add the attribute_access scanner for plain field reads.

**Risk:** Medium. New edge kinds need MCP tool surface support. Index-format compat: edges are append-only, so consumers that don't know the new edge kinds ignore them cleanly — but the LLM-facing docs need to be updated.

**Verification:**
1. Run on the fixture group. Graph neighbor query on a field entity should return ~12 caller operations (matching the grep ground truth of reads + writes).
2. Re-run bench, relevant question should flip from partial → full.

---

## 2. Pruning work (parallelizable with #1 and #2)

These four pruning fixes can land independently. They don't change the link rate or field-access answers, but they shrink the graph and clean up entity search ranking.

### Fix C — FK-target shadow nodes → edges only

**Bug shape.** A popular model exists as 281 entities: 1 legit Model + 280 unresolved FK-target placeholders. These colloquially appear as "Constraint" placeholders after reference resolution fails to resolve the foreign key.

**Where:**
- `internal/resolve/django_fk.go` — the resolver pass that already exists for the same problem only handles app-qualified strings. It needs a **global-name fallback** for cases where the model name (e.g., `User`) resolves uniquely to ONE `Model` kind across the whole index.

**What (sketch):**

In the resolver, add a strategy: if the unresolved stub's bare class name resolves uniquely to a `kind=Model` entity, rewrite the stub to that Model's ID.

**Effort:** S (half a day). Strategy is just an extra lookup.

**Risk:** Medium. Wrong-target rewrites in multi-app projects where two models have the same name (rare). Mitigate with a confidence threshold: only rewrite when exactly ONE Model entity has that bare name across the whole index.

**Verification:** post-rebuild, `archigraph_find()` on a popular model returns ≤ 5 entities (down from 281). Graph neighbor queries return real references instead of being polluted by 280 identical shadows.

### Fix D — File entity double-emit (1 088 dupes)

**Bug shape.** Every source file ends up as both `kind=SCOPE.Component(subtype=file)` and `kind=File` with different IDs and different (or empty) qualified_names.

**Where:**
- `internal/engine/commit_coupling_edges.go` — the synthetic File-entity emission. Either:
  - (preferred) **Reuse the existing `SCOPE.Component(subtype=file)` entity** as the commit-coupling endpoint; skip the secondary `File`-kind emission entirely.
  - (alternative) Keep emitting `File` but make FileEntity extraction skip emission when commit-coupling will run, OR collapse both at the resolver layer.

**What (sketch):** In the commit-coupling pass, replace the File-kind emission with a *lookup* for the existing `SCOPE.Component(subtype=file)` entity and reuse that entity's ID. Drop `Kind=File` as a synthetic kind.

**Effort:** M (1 day). Touches the commit-coupling pass + every test that asserts `Kind=File`. Need to migrate the COMMIT_COUPLED edge tests.

**Risk:** Medium. Backwards-compat for existing on-disk indexes: downstream consumers that filter `Kind=File` will need to filter `Kind=SCOPE.Component AND subtype=file` instead. Migration: keep both kinds for one release with a deprecation flag, then remove.

**Verification:** entity count drops by ~1 088 (~6% of corpus). `archigraph_find(<file_path>)` returns 1 entity, not 2.

### Fix E — error_handling:try_catch:<line> noise (1 077 nodes)

**Where:** Three extractor files emit `Name = error_handling:try_catch:<line>` as a `SCOPE.Pattern` entity per try-block.

**What (two options):**
1. **Drop entirely** — these contribute nothing structural; remove the three emitters.
2. **Normalise the name** — rename to `error_handling:try_catch:<enclosing_function_qname>` so the entity is addressable by function.

Recommend option 1 (drop). The benchmark explicitly cited these as ranking noise in entity search.

**Effort:** S (1 hour to delete + tests).

**Risk:** Low. Audit the pattern documentation and any rules that mention it. The pattern type isn't queried by MCP tools.

**Verification:** entity count drops by 1 077. Entity search no longer surfaces these patterns in top-10 results.

### Fix F — Migration over-extraction (100 nodes for 43 files)

**Where:** Extractor logic prunes the AST walk for migrations BUT still emits one entity per migration file. The inflated count likely comes from upstream passes (file entity, imports, per-operation references).

**What:** Tighten the prune. Either skip file emission for migration files altogether (loses cross-file import resolution from migrations — usually fine) OR emit a single rolled-up synthetic per `<app>/migrations/` directory.

**Effort:** S.

**Risk:** Low. Migrations have no inbound dependencies in well-formed Django apps.

**Verification:** ~95 Migration-kind entities removed.

### Fix G — Markdown heading/code-block inflation (240+ nodes)

**Where:** Markdown extractor emits SCOPE.Heading / SCOPE.CodeBlock / SCOPE.Document entities.

**What:** Add a default-deny config flag for emitting these entities beyond the markdown file itself. Make it opt-in for groups that want fine-grained docs indexing.

**Effort:** S.

**Risk:** Low. The bench showed these polluting entity search rather than providing value.

**Verification:** ~280 noise nodes removed.

---

## 3. Rollout order

```
Phase 1 — land before re-bench (REQUIRED)
  Fix A (HTTP linker) ────┐
                          ├─→ re-index fixture group
  Fix B (field edges)  ───┘
                              │
                              ▼
                      Re-run /archigraph-graph-quality with --baseline
                      Expect: relevant questions flip to full; cross-repo links rise significantly.

Phase 2 — pruning (PARALLELIZABLE, can land any time after Phase 1)
  Fix C (FK shadow → edge)
  Fix D (File double-emit)
  Fix E (try_catch nodes)
  Fix F (Migration prune tightening)
  Fix G (Markdown heading opt-in)

Phase 3 — gates
  Do NOT run /generate-docs on the fixture index until Phase 1 has shipped and the
  re-bench shows >= 80% on the same question suite (today: 75%).
```

Dependencies between fixes are minimal. The only ordering constraint is **Phase 1 must land before any docgen attempt** — that's the explicit user guidance.

Parallel-agent (latency/tokens) work is completely orthogonal to all seven fixes here. No coordination required.

---

## 4. GitHub state at time of writing

No open PRs or issues touch the HTTP linker, field-access edges, FK resolver, file double-emit, or migration prune. Closest match:

- **Quality regression test gate** — this benchmark IS the regression test. After Phase 1 ships and re-bench passes, this issue can be closed.

Open items are unrelated infra and other features. Safe to file the seven fix tickets above.

---

## 5. Open questions / things I could not confirm from source alone

- **Whether the existing generic strip actually fires in the fixture corpus.** The code path looks correct end-to-end. A minimal repro is needed. If the link is emitted in a minimal case but not on the real corpus, the bug is data-shape-specific. Fix A's url_prefix property propagation defensively handles BOTH cases.

- **Whether the MCP endpoint query actually filters as intended.** Checking the endpoint tools, a parameter may not be applied correctly when querying for definitions. Out of strict scope (this is MCP, not indexer) but worth filing alongside Phase 1.

- **The exact emitter for FK-target shadows.** The audit calls them "Constraint" because they appear as inbound shadow nodes. The most likely truth is that they're unresolved `SCOPE.External` placeholders. Fix C's strategy works for either interpretation: it adds a global Model-kind fallback before `SCOPE.External` materialisation.

---

## 6. Estimated overall effort

| Fix | Effort | Owner area |
|-----|--------|------------|
| A. HTTP linker url_prefix | S | links/ + engine/ |
| B. Field-access edges | M (phase 1) / L (phase 2) | engine/ + extractors/ |
| C. FK shadow → edge | S | resolve/ |
| D. File double-emit collapse | M | engine/ + extractors/ |
| E. try_catch node prune | S | extractors/ |
| F. Migration prune tightening | S | extractors/ |
| G. Markdown opt-in | S | extractors/ |

Total: ~3-4 engineer-days for Phase 1 + Phase 2 pruning, excluding the phase-2 attribute_access scanner (additional 3-5 days).
