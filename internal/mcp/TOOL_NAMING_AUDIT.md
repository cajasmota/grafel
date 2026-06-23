# grafel MCP — Tool naming + consolidation audit (#1742 / #1765)

Audit doc accompanying the **token-sprint bundle** PR
(#1741 #1742 #1753 #1765 #1770 #1772 #1807 #1921). Captures the per-tool
KEEP / FOLD / DESCRIPTION-SHRINK decision matrix, the rename map, and the
documented 12-tool target surface the next release converges to.

## Decision matrix (29 → 31 → target 12)

Bundle moved the registered surface from **29 → 31** to introduce
`grafel_neighbors` (folds `find_callers` + `find_callees`) while keeping
the two old names as deprecated aliases for one release (#2003 policy). Target
surface for the **next** release is **12 tools** once aliases are removed and
description-shrink #1742 Phase-C lands.

| # | Current tool                       | Decision               | Target name                | Rationale |
|---|------------------------------------|------------------------|----------------------------|-----------|
| 1 | `grafel_whoami`                | KEEP                   | `grafel_whoami`        | Session entry point; called on every handshake. |
| 2 | `grafel_status`                | KEEP                   | `grafel_status`        | Sentinel — replaces the full tool list when cwd is outside any registered group (#1769). |
| 3 | `grafel_find`                  | KEEP, rename next      | `grafel_search`        | Verb-only name violates #1765 convention; rename next release. |
| 4 | `grafel_search_entities`       | FOLD into `grafel_find` | (folded)              | Both are search APIs; merging gives one `query=` surface. Substring lookup becomes `mode=substring` on `find`. |
| 5 | `grafel_inspect`               | RENAME                 | `grafel_get_entity`    | Generic name → `get_<object>` per #1765. |
| 6 | `grafel_get_source`            | KEEP                   | `grafel_get_source`    | Already conforms to `get_<object>`. |
| 7 | `grafel_neighbors` *(new)*     | KEEP                   | `grafel_neighbors`     | Folds find_callers + find_callees (#1753). |
| 8 | `grafel_find_callers`          | KEEP                   | `grafel_find_callers`  | Un-deprecated (#5386/#5393): metrics show active use; first-class directional alias of `neighbors(direction=in)`. |
| 9 | `grafel_find_callees`          | KEEP                   | `grafel_find_callees`  | Un-deprecated (#5386/#5393): first-class directional alias of `neighbors(direction=out)`. |
|10 | `grafel_expand`                | KEEP                   | `grafel_expand`        | Un-deprecated (#5386/#5393): first-class equivalent of `neighbors(direction=both)`; agents reach for the explicit name. |
|11 | `grafel_trace`                 | KEEP                   | `grafel_shortest_path` | Verb-only name; rename next release. |
|12 | `grafel_traces`                | KEEP                   | `grafel_flows`         | Noun-only name; conflict with existing `grafel_flows` (process-flow). Fold both under one process-flow surface next release. |
|13 | `grafel_find_paths`            | KEEP                   | `grafel_find_paths`    | Verb_object — conforms. |
|14 | `grafel_subgraph`              | KEEP                   | `grafel_get_subgraph`  | Object-only; rename for verb prefix consistency next release. |
|15 | `grafel_impact_radius`         | KEEP                   | `grafel_impact_radius` | Specific enough. |
|16 | `grafel_find_dead_code`        | KEEP                   | `grafel_find_dead_code`| Verb_object — conforms. |
|17 | `grafel_quality_cycles`        | KEEP                   | `grafel_find_cycles`   | `quality_` prefix is meaningless; rename next release. |
|18 | `grafel_auth_coverage`         | KEEP                   | `grafel_auth_coverage` | Domain-specific term; keep. |
|19 | `grafel_test_coverage`         | KEEP                   | `grafel_test_coverage` | Domain-specific term; keep. |
|20 | `grafel_secrets`               | KEEP                   | `grafel_scan_secrets`  | Noun-only; rename next release. |
|21 | `grafel_module_analysis`       | KEEP                   | `grafel_module_analysis` | Domain-specific. |
|22 | `grafel_clusters`              | KEEP                   | `grafel_list_clusters` | Noun-only; rename next release. |
|23 | `grafel_stats`                 | KEEP                   | `grafel_stats`         | Top-level stats — bare noun acceptable per #1765 rule 2. |
|24 | `grafel_enrichments`           | KEEP                   | `grafel_enrichments`   | action-dispatched (list/submit/reject) — convention OK. |
|25 | `grafel_repairs`               | KEEP                   | `grafel_repairs`       | action-dispatched (list/submit). |
|26 | `grafel_apply_docgen_repairs`  | KEEP                   | `grafel_apply_docgen_repairs` | verb_object — conforms. |
|27 | `grafel_patterns`              | KEEP                   | `grafel_patterns`      | action-dispatched (query/record). |
|28 | `grafel_graph_patterns`        | KEEP                   | `grafel_list_indexer_patterns` | Confusing vs `patterns`; rename. |
|29 | `grafel_topology`              | KEEP                   | `grafel_topology`      | action-dispatched. |
|30 | `grafel_flows`                 | KEEP                   | `grafel_flows`         | action-dispatched; consolidate with `traces` next release. |
|31 | `grafel_endpoints`             | KEEP                   | `grafel_endpoints`     | action-dispatched. |

## Target 12-tool surface (next release)

After alias removal + the next-release renames above:

1. `grafel_whoami` — session bootstrap
2. `grafel_status` — sentinel (cwd outside group)
3. `grafel_search` — unified entity search (folds find + search_entities; mode=bm25|substring)
4. `grafel_get_entity` — entity-by-id (was inspect)
5. `grafel_get_source` — source snippet
6. `grafel_neighbors` — direction-discriminated graph walk (was find_callers + find_callees + expand)
7. `grafel_get_subgraph` — N-hop subgraph (raw|markdown)
8. `grafel_find_paths` — shortest path
9. `grafel_impact_radius` — blast-radius with risk
10. `grafel_find_dead_code` — orphan detector
11. `grafel_quality_bundle` — folds find_cycles + auth_coverage + test_coverage + scan_secrets behind `action=`
12. `grafel_introspect_bundle` — folds stats + clusters + module_analysis behind `action=`

Plus four `action=`-dispatched **write/queue** tools that don't count toward
the read surface: `enrichments`, `repairs`, `apply_docgen_repairs`, `patterns`.

## Sequencing

This PR ships:
- `grafel_neighbors` registration + handler (#1753)
- `fields=` on find / inspect / expand / search_entities / neighbors (#1741)
- `max_results` ceiling + `min_score` floor on find (#1807, #1921)
- `notifications/tools/list_changed` emission when registry mutates (#1772)
- `query` confirmed as canonical search arg (#1770) — already landed via #2003
- `find_callers` / `find_callees` un-deprecated and first-class again (#5386/#5393)
- Audit doc + rename map (this file) (#1765)
- Handshake budget bumped 3,200 → 3,500 to seat the additions

The hard renames + alias removal land in a **follow-up PR** to keep this one
focused on additive token-economy work that does not break agent callers.

## Anti-goals (kept out of scope this PR)

- Removing any tool the upvate field-report uses (#1742 acceptance bar).
- Hard rename of `find` / `inspect` / `expand` / `trace` — deferred to next
  release with a deprecation cycle.
- Folding `quality_*` / `secrets` / `module_analysis` under one bundle — that
  is a behavioural change requiring its own design pass.
