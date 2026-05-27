# MCP tools

archigraph exposes **43 MCP tools** (plus one cwd-gate sentinel), all prefixed `archigraph_`. The canonical source of truth for inputs, outputs, and response shapes is:

**[`internal/mcp/SCHEMA.md`](../internal/mcp/SCHEMA.md)**

This page is an orientation-level catalogue. For parameter details, response field lists, and deprecation notices, read SCHEMA.md directly.

> **Source-of-truth cross-check**: tool list verified against `internal/mcp/server.go` `registerTools()` and the `wantPresent` partition in `internal/mcp/server_test.go` `TestToolNameSurface`.

---

## Setup

After `archigraph install <group>`, the MCP server is registered automatically in your agent's config. The daemon registers one server per machine; multiple groups can be active simultaneously. The server uses stdio transport.

To verify the wiring:

```sh
archigraph status <group>    # shows MCP: connected / disconnected
```

For per-agent config details see [agent-hosts.md](agent-hosts.md).

---

## cwd resolution

All routing tools accept an optional `cwd` parameter. On **macOS (darwin) and Windows**, cwd matching is **case-insensitive** (APFS, HFS+, NTFS). On Linux it is case-sensitive. This means passing `/Users/me/Projects/MyRepo` or `/users/me/projects/myrepo` both resolve to the same group on macOS (#2545).

---

## Tool catalogue

### Status & discovery

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_whoami` | Resolve group/repo/ref for the agent's cwd. **Call this first.** |
| `archigraph_stats` | Corpus-level entity + relationship counts. Use to scope token budgets. |
| `archigraph_search_entities` | Substring search over entity names; ranked matches with source locations. |
| `archigraph_find` | BM25-ranked graph query with optional BFS expansion. Primary discovery tool. |
| `archigraph_inspect` | Look up a single entity by ID, qualified name, or label. Returns full record + findings. |
| `archigraph_get_source` | Return actual source lines for an entity; accepts id, qualified_name, or label. |
| `archigraph_neighbors` | Graph neighbors of an entity. `direction=in\|out\|both` (default `both`). Supersedes `find_callers`/`find_callees`. |

#### `archigraph_whoami`

**When to call**: every session start, before any other graph call.

Key parameters: `cwd` (optional; inferred from shell), `group` (optional override), `ref` (optional git ref).

Output: `cwd_resolved_to`, `group`, `repo`, `indexed_ref`, `is_worktree`.

#### `archigraph_stats`

Key parameters: `group`/`cwd`, `repo_filter[]`, `breakdown` (`"unresolved_imports"` adds edge taxonomy).

Output: entity counts per kind, relationship counts, unresolved import breakdown when requested.

#### `archigraph_search_entities`

Key parameters: `query` (required), `kind_filter`, `limit` (default 30), `repo_filter[]`.

Output: ranked list of matching entities with source file + line.

#### `archigraph_find`

Key parameters: `query` (required), `mode` (`bfs`/`ids`), `depth` (default 3), `token_budget` (default 800), `max_results` (default 50, ceiling 200), `min_score` (default 0.15), `repo_filter[]`, `context_filter[]`, `fields[]`.

Output: BM25-scored entities with BFS expansion. Tail trimmed below `min_score`.

#### `archigraph_inspect`

Key parameters: `entity_id` (required; accepts id, qname, or label), `verbose` (bool), `repo_filter[]`, `fields[]`.

Output: full entity record including all properties + attached findings.

#### `archigraph_get_source`

Key parameters: `entity_id` (required), `context_lines` (default 20).

Output: source text with start/end line numbers. Times out gracefully on large files.

#### `archigraph_neighbors`

Key parameters: `entity_id` (required), `direction` (`in`/`out`/`both`, default `both`), `depth` (default 1), `token_budget` (default 800), `fields[]`.

Output: list of neighboring entities with edge kind + direction.

---

### Graph traversal

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_expand` | *Deprecated alias* of `archigraph_neighbors`. Returns neighbors of `entity_id`. |
| `archigraph_find_callers` | *Deprecated alias* of `archigraph_neighbors(direction=in)`. Ranked by call frequency. |
| `archigraph_find_callees` | *Deprecated alias* of `archigraph_neighbors(direction=out)`. |
| `archigraph_find_paths` | Shortest path between two entities with confidence score. |
| `archigraph_trace` | Confidence-weighted shortest path (Dijkstra) between two nodes. |
| `archigraph_traces` | Pre-computed process-flow traces. `action=list\|get\|follow`. |
| `archigraph_subgraph` | Nodes+edges within N hops (`format=raw`) or Markdown summary (`format=markdown`). |

#### `archigraph_find_callers` — behavioral note (#2577/#2591)

Results are **ranked by call frequency** (descending) within each hop level, then alphabetically. Frequency is summed from `Properties["count"]` on CALLS edges (or 1.0 per raw edge when count is absent). Tie-break is alphabetical by name.

> **Deprecation**: prefer `archigraph_neighbors(direction=in)` for new code.

#### `archigraph_find_paths`

Key parameters: `from` (required), `to` (required), `max_hops` (default 5).

Output: path nodes + edges with per-hop confidence.

#### `archigraph_trace`

Key parameters: `source` (required), `target` (required), `repo_filter[]`.

Output: Dijkstra shortest path with confidence weights.

#### `archigraph_traces`

Key parameters: `action` (`list`/`get`/`follow`, default `list`), `process_id`, `entry_point_id`, `max_depth` (default 8), `limit` (default 10), `token_budget` (default 800).

Output: process-flow trace records; `follow` does cross-stack BFS from an entry point.

#### `archigraph_subgraph`

Key parameters: `entity_id` (required), `depth` (default 2), `format` (`raw`/`markdown`, default `raw`).

Output: nodes+edges JSON (`raw`) or human-readable Markdown summary (`markdown`).

---

### Cross-cutting analysis

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_cross_links` | Cross-repo link candidates: `list=pending`, `accept\|reject=resolve`. |
| `archigraph_endpoints` | HTTP endpoints: `definitions\|calls\|stats`. Filter by `path_contains`+`method`. |
| `archigraph_clusters` | Louvain communities with top-ranked entities. Fast module map. |
| `archigraph_module_analysis` | Module-level SCC + PageRank + betweenness. `action=cycles\|centrality\|all`. |
| `archigraph_topology` | Message-channel topology: orphan publishers/subscribers, topic detail. |
| `archigraph_flows` | Flow-process diagnostics: `dead_ends`, `truncated`, `detail`. |
| `archigraph_graph_patterns` | Indexer-extracted structural patterns (not agent store): `list\|get`. |

#### `archigraph_cross_links`

Key parameters: `action` (required: `list`/`accept`/`reject`), `channel`, `method`, `limit`, `candidate_id`, `override_target` (read from request map, undeclared to stay under token ceiling).

Output: cross-repo HTTP/Kafka/WS link records with match confidence.

#### `archigraph_endpoints`

Key parameters: `action` (required: `definitions`/`calls`/`stats`), `path_contains`, `method`, `orphan_only`, `limit` (default 20), `offset` (default 0), `token_budget` (default 800), `format` (`terse`/`full`).

Filters (`path_contains`, `method`) are applied **before** `limit`.

#### `archigraph_clusters`

Key parameters: `repo_filter[]`, `top_entities_limit` (default 3), `min_size` (default 20).

Output: list of community clusters with representative entities.

#### `archigraph_module_analysis`

Key parameters: `action` (`cycles`/`centrality`/`all`, default `all`), `top_n`, `limit`, `min_size`, `repo_filter[]` (undeclared extras read from request map).

Output: module SCCs (cycles), PageRank + betweenness centrality scores.

#### `archigraph_topology`

Key parameters: `action` (required: `orphan_publishers`/`orphan_subscribers`/`topic_detail`/`topics`), `topic_id`, `repo_filter[]`, `verbose`.

#### `archigraph_flows`

Key parameters: `action` (required: `dead_ends`/`truncated`/`detail`/`list`), `process_id`, `repo_filter[]`.

#### `archigraph_graph_patterns`

Key parameters: `action` (required: `list`/`get`), `pattern_id`, `needs_attention` (bool), `status`, `confidence_min`, `limit` (default 50), `repo_filter[]`.

---

### Findings & docs

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_save_finding` | Persist a Q&A pair to the group memory store. |
| `archigraph_list_findings` | Read back saved findings, optionally filtered. |
| `archigraph_docgen_start_run` | Start or resume a local-staging docgen run. Returns `run_id` + `staging_path`. |
| `archigraph_docgen_status` | Inspect an in-flight docgen run: files written + SHA-256 per file. |
| `archigraph_docgen_validate` | Lint frontmatter + cross-links. Read-only. |
| `archigraph_docgen_promote` | Atomic staging → canonical rename. Blocks SSG scaffolding. |
| `archigraph_docgen_abort` | Cancel a staging run: rm -rf staging, release per-group lock. |
| `archigraph_docgen_list` | List canonical doc files under `~/.archigraph/docs/<group>/`. |
| `archigraph_persona_event` | Record persona lifecycle events (invoke/consult_out/save_finding). **LOCAL ONLY.** |

#### `archigraph_save_finding` / `archigraph_list_findings`

`save_finding` key parameters: `question` (required), `answer` (required); optional `type`, `nodes[]`, `repo_filter[]`.

`list_findings` optional extras: `since` (RFC3339), `entity_id`, `limit`.

Output stored at `~/.archigraph/findings/<group>/`.

#### docgen workflow

Standard flow: `start_run` → write files into `staging_path` → `validate` → `promote`. Use `abort` to reset a failed run. Use `status` to check progress mid-run.

`archigraph_docgen_start_run` key parameters: `group` (required), `resume` (default `true`), `no_git` (default `false`).

`archigraph_docgen_promote` key parameters: `run_id` (required), `force` (default `false`).

#### `archigraph_persona_event` (new — #2474)

Records persona lifecycle telemetry to `~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl`. **Data never leaves the local machine.**

Key parameters: `persona` (required), `event_type` (required: `invoke`/`consult_out`/`save_finding`), `target_persona` (for `consult_out`), `metadata`.

**When to call**: at session start (`event_type=invoke`) and on each Consult-Out. Group-agnostic — no `cwd` routing needed.

---

### Audit

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_license_audit` | Audit dependency licenses; flag GPL/AGPL conflicts. |
| `archigraph_secrets` | Scan for hardcoded secrets; masked findings by severity. |
| `archigraph_quality_cycles` | Detect import cycles via Tarjan SCC; weakest edge + fix hint. |
| `archigraph_test_coverage` | Production entities with no TESTS edge, ranked by severity. |
| `archigraph_auth_coverage` | Flag HTTP endpoints missing auth (severity, IDOR risk). |
| `archigraph_find_dead_code` | Entities with no project edges — dead code or extraction gap candidates. |
| `archigraph_impact_radius` | Inbound blast-radius: affected entities with `risk_score [0,1]`. |

#### `archigraph_license_audit`

Key parameters: `group`/`cwd`; optional undeclared extras: `include_transitive`, `severity`, `limit`.

Output: dependency records flagged by license kind (GPL/AGPL conflict detection).

#### `archigraph_secrets`

Key parameters: `severity`, `limit` (default 200). Accepts `group`/`cwd` but routing is optional.

Output: masked credential findings by severity (error/warn/info). Test fixtures and opt-out comments suppressed.

#### `archigraph_quality_cycles`

Key parameters: `repo_filter[]`, `limit` (default 100).

Output: SCC lists representing circular import chains, with weakest edge identified and a suggested fix.

#### `archigraph_test_coverage`

Key parameters: `entity_id` (optional — scoped query), `repo_filter[]`, `severity`, `limit` (default 100), `top_directories` (bool).

Output: production entities lacking TESTS edges, ranked by severity.

#### `archigraph_auth_coverage`

Key parameters: `repo_filter[]`, `only_missing` (bool, default `false`), `limit` (default 200).

Output: per-endpoint auth status with severity (error = sensitive/IDOR, warn = unauthenticated public, info = covered).

#### `archigraph_find_dead_code`

Key parameters: `repo_filter[]`, `kind_filter`, `limit` (default 100).

Output: entities with zero project edges; may be genuine dead code or an extractor gap.

#### `archigraph_impact_radius`

Key parameters: `entity_id` (required), `hops` (default 2).

Output: list of affected entities with `risk_score [0,1]` (higher = more transitive dependents).

---

### Admin & repair

| Tool | One-line purpose |
|------|-----------------|
| `archigraph_apply_docgen_repairs` | Docgen feedback: apply repair candidates to graph enrichments. |
| `archigraph_enrichments` | Enrichment candidates: `list=pending`, `submit=resolve`, `reject=discard`. |
| `archigraph_repairs` | Residual-edge repair queue: `list=pending`, `submit=resolve`. |
| `archigraph_diff_refs` | Diff two indexed git refs: added/removed/modified entities + relationships. |
| `archigraph_patterns` | Agent pattern store (ADR-0018): `query=find by task`, `record=store with exemplars`. |
| `archigraph_mcp_metrics` | Session tool-call metrics (counts, p50/p95 ms) + last N days rollups. |

#### `archigraph_enrichments`

Key parameters: `action` (required: `list`/`submit`/`reject`), `kind`, `limit` (default 10), `candidate_id`, `value`, `confidence`, `reason`, `repo_filter[]`.

#### `archigraph_repairs`

Key parameters: `action` (required: `list`/`submit`), `repo_filter[]`, `limit` (default 20), `offset` (default 0). Submit extras (read from request map, undeclared): `residual_id`, `resolution`, `target_entity_id`, `module`, `new_target`, `dynamic_reason`, `abandon_reason`, `confidence`, `reasoning`.

#### `archigraph_apply_docgen_repairs`

Key parameters: `repo_filter[]`, `dry_run` (bool). Applies docgen-discovered enrichment candidates in a single batch.

#### `archigraph_diff_refs`

Key parameters: `group`, `repo` (required), `ref_a` (required), `ref_b` (required).

Output: added/removed/modified entities and relationships between the two indexed refs.

#### `archigraph_patterns`

Key parameters: `action` (required: `query`/`record`), `text` (query text), `category`, `limit` (default 10), `steps[]`, `exemplars[]`.

Note: distinct from `archigraph_graph_patterns` (indexer-extracted). This is the **agent-learned** pattern store (ADR-0018).

#### `archigraph_mcp_metrics` (new — #2529)

Returns in-memory per-tool counters for the **current daemon session** plus up to N days of persisted daily rollup records from `~/.archigraph/metrics/mcp-YYYY-MM-DD.jsonl`.

Key parameters: `days` (default 3). Group-agnostic — no `cwd` routing needed.

Output fields: per-tool `calls`, `errors`, `p50_ms`, `p95_ms`; daily rollup records with the same shape.

---

## Sentinel tool

`archigraph_status` is registered as a real callable tool but is shown **only** when the agent's `cwd` falls outside all registered groups. It returns guidance on how to configure a group. It does not appear in the normal tool handshake for indexed sessions.

---

## Deprecated & removed tools

Tools that existed in earlier releases but are no longer registered:

| Tool | Replacement |
|------|-------------|
| `archigraph_expand` | `archigraph_neighbors` (kept as alias; will be removed in a future release) |
| `archigraph_find_callers` | `archigraph_neighbors(direction=in)` |
| `archigraph_find_callees` | `archigraph_neighbors(direction=out)` |
| `archigraph_recent_activity` | Removed; filter `archigraph_find` by timestamp |
| `archigraph_quality` | Split into `archigraph_quality_cycles`, `archigraph_find_dead_code`, `archigraph_auth_coverage` |
| `archigraph_diagnostics` | Dashboard-only; use HTTP `/api/diagnostics` |
| `archigraph_get_telemetry` | Dashboard-only; use HTTP `/api/telemetry` |
| `archigraph_get_next_enrichment_task` | `archigraph_enrichments(action=list, limit=1)` |
| `archigraph_quality_orphans` | `archigraph_find_dead_code` |
| `archigraph_get_subgraph` / `archigraph_summarize_subgraph` | `archigraph_subgraph` |
| `archigraph_docgen_cancel` | `archigraph_docgen_abort` |

Old tool names were changed in #668 and #1281. Old names return a clear `"tool not found"` error (no silent fallback, per ADR-0017).

---

## Pairing with grep

archigraph MCP and grep are complementary. Use MCP for structural questions (who calls X, trace a flow, find callers). Use grep for raw enumeration (every `if err != nil`, every import line). See [CLAUDE.md](../CLAUDE.md) for the pairing guide with worked examples.
