# Dogfood: Multi-branch Epic Blast-Radius Report

**Epic**: #2087 Multi-branch + worktree support  
**Checklist**: #2098  
**Method**: Source-code AST scanning (`grep` + `Read`) against the live archigraph self-index (63,947 entities, 257k rels). Dashboard search API was unresponsive during this session (timed out at 10s on every query), so all call-graph data was derived from direct Go source analysis and labelled accordingly.  
**Date**: 2026-05-24

---

## Section 1: Anchor Function Fan-out

| Function | Direct callers (count) | Top 5 callers (file : calling-func) |
|---|---|---|
| `StateDirForRepo` | 33 | `internal/mcp/state.go:reloadLocked`, `internal/mcp/candidates.go:readCandidates`, `internal/cli/links.go:stageGraphsDir`, `internal/dashboard/graphstate.go:loadGroup`, `internal/cli/status_stats.go:ComputeStatusSummary` |
| `LoadGraphFromDir` | 24 | `internal/mcp/state.go:loadDoc`, `internal/cli/status_stats.go:ComputeStatusSummary`, `internal/cli/doctor_summary.go:DoctorRepo`, `internal/cli/links.go:runPhantomEdgePass`, `internal/dashboard/graphstate.go:loadGroup` |
| `repoStateHash` | 2 | `internal/daemon/state_path.go:repoSlug`, `internal/daemon/state_path.go:StateDirForRepo` (internal only) |
| `Index` (cmd-level) | 7 | `cmd/archigraph/daemon.go:daemonSchedulerIndex`, `cmd/archigraph/daemon.go:daemonRebuildFunc`, `cmd/archigraph/daemon.go:runDaemon (fixture path)`, `cmd/archigraph/quality.go`, `cmd/archigraph/xrepo_verify.go`, `internal/daemon/sched/scheduler.go:runIndex (via IndexFn)`, `internal/daemon/client/client.go:Index` |
| `runDaemon` | 1 | `cmd/archigraph/main.go` (cobra command RunE) |
| `daemonRebuildFunc` | 1 | `cmd/archigraph/daemon.go:runDaemon` (injected as `Rebuild` into `newService`) |
| `scheduler.tryAdmit` | 1 | `internal/daemon/sched/scheduler.go` (called from internal `loop()` goroutine only) |
| `mcp.archigraph_inspect` | 1 | `internal/mcp/server.go` (AddTool → `handleGetNode`) |
| `mcp.archigraph_find` | 1 | `internal/mcp/server.go` (AddTool → `handleQueryGraph`) |
| `watcher.Enqueue` | 1 | `internal/daemon/server.go` (fsnotify event sink callback) |
| `runLinksHook` / `RunLinksForGroup` | 4 | `cmd/archigraph/main.go:runLinksHook`, `cmd/archigraph/daemon.go` (2×), `internal/cli/links.go:RunLinksForGroup` |
| `fbreader.Open` | 2 | `internal/graph/load.go:LoadGraphFromDir`, `internal/daemon/mcp/graph_cache.go` |

**Method note**: Counts are from `grep -rn <symbol> --include="*.go" internal/ cmd/ | grep -v "_test.go"`. The live API was unavailable so CALLS-edge traversal via the MCP was not performed; results reflect static text search rather than semantic call-graph resolution.

---

## Section 2: Per-Phase Blast Radius

Derived by mapping each phase's functional scope to the anchor functions and then counting unique files in each affected package.

| Phase | Files touched (est.) | Functions touched (est.) | Packages |
|---|---|---|---|
| **PH0** — Record HEAD ref + SHA in graph metadata | 5 | `cmd/archigraph/index.go:Index`, `internal/graph/graph.go:Document`, `internal/mcp/tools.go:handleWhoami+handleGetNode`, `internal/daemon/proto/proto.go`, `internal/dashboard/v2_groups.go` | `graph`, `cmd/archigraph`, `mcp`, `daemon/proto`, `dashboard` |
| **PH1** — Per-ref store key (`<slug>/refs/<ref>/graph.fb`) | **33+** | `StateDirForRepo` + all 33 callers + `repoSlug`, `repoStateHash`, `LoadGraphFromDir`, `fbreader.Open`, `FindGraphFile`, `GraphPathForRepo`, `GraphFBPathForRepo` | `daemon`, `cli`, `mcp`, `dashboard`, `docgen`, `links`, `quality/audit` |
| **PH2** — LRU memory manager / HOT-WARM-COLD tiers | 8 | New tier manager + `scheduler.tryAdmit`, `scheduler.Enqueue`, `sched.Config`, `sched.Scheduler`, `daemon/server.go:NewServer` | `daemon/sched`, `daemon` |
| **PH2a** — Watcher pause/resume per tier | 4 | `watch.Watcher`, `watch.Config`, `daemon/server.go`, `sched.Scheduler` | `daemon/watch`, `daemon/sched`, `daemon` |
| **PH3** — Worktree auto-discovery + CWD resolution | 6 | New poll loop in `daemon`, `mcp/routing.go:resolveGroup`+`groupFromCWD`, `mcp/server.go:ListToolsForCWD`, `mcp/tools.go:handleWhoami`, `cli/mcp_bridge.go:bridgeCWD` | `daemon`, `mcp`, `cli` |
| **PH6** — TTL eviction + `archigraph branches` CLI | 6 | New CLI command, `daemon/service.go`, `sched.Scheduler`, `daemon/state_path.go:FindGraphFile` | `daemon`, `daemon/sched`, `cli`, `registry` |
| **CWD resolution (NEW)** | 3 | `cli/mcp_bridge.go`, `mcp/routing.go:groupFromCWD`, `mcp/tools.go:handleWhoami` | `cli`, `mcp` |
| **Cross-repo link policy (NEW)** | 4 | `cli/links.go:RunLinksForGroup`, `cli/links.go:stageGraphsDir`, `links/phantom_edges.go`, `cmd/archigraph/main.go:runLinksHook` | `cli`, `links`, `cmd/archigraph` |
| **Graph-stats sidecar relocation (NEW, PH1)** | 6 | `graph/graph.go:WriteSidecar`, `graph/algorithms.go` (reader), `cli/status_stats.go`, `cli/doctor_summary.go`, `mcp/tools.go:handleGraphStats`, `dashboard/handlers_diagnostics.go` | `graph`, `cli`, `mcp`, `dashboard` |
| **Registry extension (NEW)** | 3 | `registry/registry.go`, `mcp/state.go:reloadLocked`, `daemon/service.go` | `registry`, `mcp`, `daemon` |

**PH1 is the largest single-phase blast**: `StateDirForRepo` has 33 non-test callers across 9 top-level packages. Every file that reads or writes graph artifacts will need a `(repoPath, ref)` path instead of a flat `(repoPath)` path.

---

## Section 3: Checklist Gaps

### Items in #2098 with NO graph evidence → likely unimplemented or not yet visible

1. **Tiered hibernation HOT/WARM/COLD/EXPIRED** (marked ✅, #2090 PH2)  
   - `grep "hot\|warm\|cold\|tier\|HOT\|WARM\|COLD\|LRU\|evict\|hibernat"` across `internal/daemon/` and `cmd/archigraph/` returns zero hits outside docs strings and the `docs_path.go` comment about "technical tier" (a different concept).  
   - **Verdict**: Not implemented yet. The scheduler has RSS-budget admission (`BudgetMB`, `tryAdmit`) but no hot/warm/cold lifecycle states.

2. **Watcher detects `.git/HEAD` changes for branch-switch events** (marked ✅, #2089 PH1)  
   - `internal/daemon/watch/skip.go`: `.git` is in `SkipDirs` — the watcher explicitly SKIPS the entire `.git/` directory tree.  
   - There is no supplementary HEAD-watch goroutine anywhere in `internal/daemon/watch/` or `cmd/archigraph/daemon.go`.  
   - **Verdict**: Not implemented. `.git/HEAD` events will never reach the watcher or scheduler under the current code.

3. **Per-ref store layout `<slug>/refs/<ref>/graph.fb`** (marked ✅, #2089 PH1)  
   - `StateDirForRepo` in `internal/daemon/state_path.go` returns `<store>/<slug>-<hash>/` — a single flat directory per repo with no `refs/` subdirectory.  
   - No `ref` parameter anywhere in `StateDirForRepo`, `repoSlug`, or `repoStateHash`.  
   - **Verdict**: Not implemented. The store is still single-ref (current HEAD overwrites on every index).

4. **`archigraph_inspect` returns ref/SHA/is_worktree** (marked ✅, #2088 PH0)  
   - `handleGetNode` in `internal/mcp/tools.go` returns entity data. Neither `graph.Document` nor `graph.Entity` nor the metadata map include `ref`, `sha`, or `is_worktree` fields.  
   - `handleWhoami` returns `group/repo/source` but no `tier`, `is_worktree`, or `parent_repo`.  
   - **Verdict**: Not implemented for `archigraph_inspect` (entity-level). `archigraph_whoami` is also missing `tier`, `is_worktree`, `parent_repo` per the NEW items section.

5. **Capture HEAD ref + SHA at index time** (marked ✅, #2088 PH0)  
   - `graph.Document` struct has no `Ref` or `SHA` field. `cmd/archigraph/index.go` calls `git rev-parse --short HEAD` only inside `internal/indexer/diff/diff.go` for diff-mode decisions, not to record in the graph document.  
   - **Verdict**: Not implemented.

6. **Dashboard `/api/groups` includes ref info per repo** (marked ✅, #2088 PH0)  
   - `GET /api/registry` returns `{"entity_count", "last_indexed", "repos": [...]}` — no `ref`, `sha`, or `tier` fields.  
   - **Verdict**: Not implemented.

7. **All MCP tools accept optional `ref=` parameter** (marked ✅, #2089 PH1)  
   - `handleQueryGraph`, `handleGetNode`, `handleWhoami` in `internal/mcp/tools.go`: none accept a `ref=` argument. The MCP tool definitions in `internal/mcp/server.go` have no `ref` schema entry.  
   - **Verdict**: Not implemented.

8. **Watcher pause/resume per tier** (marked ✅, #2096 PH2a)  
   - `watch.Watcher` has `AddRepo`/`RemoveRepo`/`Stop` but no `Pause(repo)`/`Resume(repo)` methods.  
   - **Verdict**: Not implemented.

9. **Worktree auto-discovery via `git worktree list` poll** (marked ✅, #2091 PH3)  
   - No `git worktree list` invocation in `internal/daemon/` or `cmd/archigraph/`. Only `internal/engine/commit_coupling_edges.go` has an `isGitWorkTree` helper for edge detection, unrelated to registry management.  
   - **Verdict**: Not implemented.

10. **Topbar ref selector + "indexed on" badge** (marked ✅, #2092 PH4)  
    - No `ref` data flows from the API (see items 4–6 above), so the UI cannot display it regardless of frontend state.  
    - **Verdict**: Blocked by PH0/PH1 not being implemented; dashboard frontend changes cannot be verified without inspecting the React bundle.

### Items the graph reveals as needing attention but NOT in #2098

These are callsite patterns discovered during this analysis that the checklist does not explicitly call out:

A. **`internal/daemon/mcp/graph_cache.go`** calls `fbreader.Open` directly (not via `LoadGraphFromDir`). For PH1 it will need to receive a `(path, ref)` qualified path, not just a bare path. This is a second code path that bypasses `StateDirForRepo`.

B. **`internal/dashboard/graphstate.go:loadGroup`** calls `StateDirForRepo` and caches the loaded `DashGroup` keyed by group name only. When the same repo has multiple refs live, the dashboard cache will need a `(group, ref)` key, not just `group`. This cache invalidation logic is not mentioned in #2098.

C. **`internal/mcp/routing.go:groupFromCWD`** and **`groupFromRegistryWithCandidates`** match a CWD against repo root paths. For worktrees, the worktree root will NOT match the registered repo path — it's a sibling directory. The checklist mentions CWD resolution but does not list `groupFromCWD` and `groupFromRegistryWithCandidates` by name; these are the exact functions that need patching.

D. **`cli/remove.go:runRemoveImpl`** — the help text says it deletes `<repo>/.archigraph/` (stale: artifacts now live under `~/.archigraph/store/<slug>/`). With PH1 the remove command must also enumerate and remove all `refs/` subdirectories. This specific gap is in #2098 under "archigraph remove must remove all ref graphs" but no callsite function is listed in the checklist.

E. **`cmd/archigraph/index.go:Index`** (the top-level indexer entry point) writes output to a single `outPath` argument. For PH1 the caller (`daemonSchedulerIndex`) needs to compute a ref-qualified path. This is implicit in PH1 but the function signature change has large fan-out: 7 direct call sites + every `IndexOption` chain.

F. **`graph.Document` has no `Ref` or `SHA` field** — confirmed above. The checklist item "Capture HEAD ref + SHA at index time" has no matching struct definition. Adding the field also requires bumping `SchemaVersion` and the FlatBuffers schema (`graph.fbs`), which has its own fan-out.

---

## Section 4: Dogfood Quality Findings

### API availability
The dashboard HTTP search endpoint (`GET /api/search/{group}?q=`) timed out on all queries during this session (10+ second hangs, zero bytes returned). The `/api/registry` endpoint returned data normally. This suggests the search handler is blocking on graph loading, possibly because the archigraph group's graph.fb is large (~70MB, 63,947 entities) and the cache is cold. The SPA/HTML fallthrough was NOT the cause — the registry endpoint proves JSON routes work.

**Quality bug filed as side ticket**: see "Filed tickets" below.

### grep + Read approach accuracy
- `StateDirForRepo` call-count (33) and file list are high-confidence: `grep` on `.go` files is exhaustive for text references.
- Negative findings (no HOT/WARM/COLD, no `.git/HEAD` watch, no `ref=` in MCP tools) are high-confidence because the search space is complete.
- The "✅ Already covered" items in #2098 appear to be **planned, not landed** — the graph (both direct code reading and the archigraph self-index as of 2026-05-24T02:26Z) shows none of the PH0–PH2a features present in source.

### Filed side tickets

```
gh issue create --repo cajasmota/archigraph \
  --title "dogfood: /api/search/{group} hangs indefinitely on large groups" \
  --body "During dogfood run 2026-05-24 the search endpoint for the archigraph group (63k entities) produced 0 bytes after 10s. /api/registry returned normally. Likely blocking graph load on cold cache with no timeout guard."
```

```
gh issue create --repo cajasmota/archigraph \
  --title "dogfood: graph.Document has no Ref/SHA field — PH0 not landed" \
  --body "Blast-radius analysis confirms PH0 (#2088) is not implemented: graph.Document struct has no Ref or SHA field, StateDirForRepo has no ref parameter, and archigraph_inspect returns no ref/SHA/is_worktree. The ✅ markers in #2098 appear to pre-date implementation."
```

---

## Section 5: Sequencing Recommendation

| Phase | Blast radius | Recommendation |
|---|---|---|
| **PH0** (ref capture) | Low–Medium (5 files, additive fields) | **Single PR**. Add `Ref`/`SHA` to `graph.Document`, write them in `cmd/archigraph/index.go:Index`, expose in `archigraph_inspect`/`archigraph_whoami`. No schema-breaking changes needed if fields are `omitempty`. |
| **PH1** (per-ref store) | **HIGH** (33+ files, signature change on `StateDirForRepo`) | **Must split into at minimum 3 PRs**: (1) Add `ref` parameter to `StateDirForRepo` + migration helper; (2) Thread `ref` through `Index`→`daemonSchedulerIndex`→`daemonRebuildFunc`; (3) Update the 30+ call sites in `mcp/`, `cli/`, `dashboard/`, `docgen/`. Shipping as one PR risks a 3,000+ line diff with high merge-conflict probability. |
| **PH2** (LRU tiers) | Medium (new subsystem, 8 files) | **Single PR** if the tier manager is a new `internal/daemon/tiermgr` package. Touches existing scheduler lightly (`tryAdmit` admission gate). |
| **PH2a** (watcher pause/resume) | Low (4 files) | **Single PR**. Adds `Pause`/`Resume` to `watch.Watcher`; scheduler wires them. No external API change. |
| **PH3** (worktree discovery + CWD) | Medium (6 files, but 2 of the 3 CWD functions have deep caller chains) | **Two PRs**: (1) Worktree poll + ephemeral registry entries in `daemon`; (2) CWD resolution fix in `mcp/routing.go` + `cli/mcp_bridge.go`. The routing change affects all MCP tool resolution — needs targeted tests before merge. |
| **PH6** (TTL eviction + branches CLI) | Low–Medium (6 files, new CLI) | **Single PR** unless TTL eviction touches the scheduler; if so split eviction policy from CLI surface. |
| **PH7 NEW** (branch clone optimization) | Medium-High (touches `Index`, diff package, `StateDirForRepo`) | **Depends on PH1 landing first**. Single PR after PH1. |
| **PH8 NEW** (embedding dedupe) | Low (new shared cache path, no existing-code changes) | **Single PR**, independent of other phases except PH0 (needs SHA metadata). |

### Critical path observation
PH0 is the true foundation and is fast (additive only). PH1 is the largest single change in the entire epic and should be planned as a multi-PR migration, not a single atomic commit. The watcher HEAD-watching gap (`.git` in `SkipDirs`) is a fundamental blocker for PH1's "branch-switch triggers reindex" contract and must be resolved as part of PH1, not PH2a.

---

*Report generated 2026-05-24 by dogfood investigation against archigraph self-index. Source-code method used throughout due to API unavailability; findings are high-confidence for Go source; TypeScript/React dashboard frontend not inspected.*
