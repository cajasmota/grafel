# MCP tools

archigraph exposes **65 MCP tools** (plus one cwd-gate sentinel, `archigraph_status`), all prefixed `archigraph_`. The canonical source of truth for inputs, outputs, and response shapes is:

**[`internal/mcp/SCHEMA.md`](../internal/mcp/SCHEMA.md)**

This page is the orientation-level **index**: one row per tool grouped by category, each linking to a per-category detail page with parameters and examples. For exhaustive response-field lists read SCHEMA.md directly.

> **Source-of-truth cross-check**: the tool list is verified against the `AddTool` registrations in [`internal/mcp/server.go`](../internal/mcp/server.go) and the `wantPresent` partition in `internal/mcp/server_test.go` `TestToolNameSurface`.

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

Most tools also accept the common routing arguments `group`, `cwd`, and `ref` (optional git ref). See [SCHEMA.md → Common arguments](../internal/mcp/SCHEMA.md) for the full shared-parameter list.

---

## Tool catalogue

Tools are grouped into seven categories. Click a category heading for the detail page; each table lists every tool in that category with a one-line purpose.

### [Status & discovery](mcp-tools/status-and-discovery.md)

Orientation, search, and single-entity lookup. **Call `archigraph_whoami` first.**

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_whoami`](mcp-tools/status-and-discovery.md#archigraph_whoami) | Resolve group/repo/ref for the agent's cwd. **Call this first.** |
| [`archigraph_stats`](mcp-tools/status-and-discovery.md#archigraph_stats) | Corpus-level entity + relationship counts. Use to scope token budgets. |
| [`archigraph_orient`](mcp-tools/status-and-discovery.md#archigraph_orient) | Orientation analysis: key entities, cross-cutting edges, orientation questions. |
| [`archigraph_search_entities`](mcp-tools/status-and-discovery.md#archigraph_search_entities) | Substring search over entity names; ranked matches with source locations. |
| [`archigraph_find`](mcp-tools/status-and-discovery.md#archigraph_find) | BM25-ranked graph query with optional BFS expansion. Primary discovery tool. |
| [`archigraph_inspect`](mcp-tools/status-and-discovery.md#archigraph_inspect) | Look up a single entity by id/qname/label. Full record + line-precise calls/called_by. |
| [`archigraph_get_source`](mcp-tools/status-and-discovery.md#archigraph_get_source) | Return actual source lines for an entity; accepts id, qualified_name, or label. |
| [`archigraph_subgraph`](mcp-tools/status-and-discovery.md#archigraph_subgraph) | Nodes+edges within N hops (`format=raw`) or Markdown summary (`format=markdown`). |

### [Graph traversal](mcp-tools/graph-traversal.md)

Walk edges, find paths, and follow flows between entities.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_neighbors`](mcp-tools/graph-traversal.md#archigraph_neighbors) | Graph neighbors of an entity. `direction=in\|out\|both` (default `both`). |
| [`archigraph_expand`](mcp-tools/graph-traversal.md#archigraph_expand) | *Deprecated alias* of `archigraph_neighbors`. |
| [`archigraph_find_callers`](mcp-tools/graph-traversal.md#archigraph_find_callers) | *Deprecated alias* of `archigraph_neighbors(direction=in)`. Ranked by call frequency. |
| [`archigraph_find_callees`](mcp-tools/graph-traversal.md#archigraph_find_callees) | *Deprecated alias* of `archigraph_neighbors(direction=out)`. |
| [`archigraph_find_paths`](mcp-tools/graph-traversal.md#archigraph_find_paths) | Shortest path between two entities with confidence score. |
| [`archigraph_trace`](mcp-tools/graph-traversal.md#archigraph_trace) | Confidence-weighted shortest path (Dijkstra) between two nodes. |
| [`archigraph_traces`](mcp-tools/graph-traversal.md#archigraph_traces) | Pre-computed process-flow traces. `action=list\|get\|follow`. |
| [`archigraph_navigates`](mcp-tools/graph-traversal.md#archigraph_navigates) | NAVIGATES_TO edge query: filter by route/param, direction, multi-hop flow. |

### [Cross-cutting analysis](mcp-tools/cross-cutting-analysis.md)

Modules, communities, HTTP surface, topology, flows, and impact.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_clusters`](mcp-tools/cross-cutting-analysis.md#archigraph_clusters) | Louvain communities with top-ranked entities. Fast module map. |
| [`archigraph_module_analysis`](mcp-tools/cross-cutting-analysis.md#archigraph_module_analysis) | Module-level SCC + PageRank + betweenness. `action=cycles\|centrality\|all`. |
| [`archigraph_import_cycles`](mcp-tools/cross-cutting-analysis.md#archigraph_import_cycles) | IMPORTS cycle clusters per repo (Tarjan SCC). |
| [`archigraph_quality_cycles`](mcp-tools/cross-cutting-analysis.md#archigraph_quality_cycles) | Detect import cycles via Tarjan SCC; weakest edge + fix hint. |
| [`archigraph_impact_radius`](mcp-tools/cross-cutting-analysis.md#archigraph_impact_radius) | Inbound blast-radius: affected entities with `risk_score [0,1]`. |
| [`archigraph_pr_impact`](mcp-tools/cross-cutting-analysis.md#archigraph_pr_impact) | PR impact + merge-risk: changes → communities → blast radius. |
| [`archigraph_diff_refs`](mcp-tools/cross-cutting-analysis.md#archigraph_diff_refs) | Diff two indexed git refs: added/removed/modified entities + relationships. |
| [`archigraph_endpoints`](mcp-tools/cross-cutting-analysis.md#archigraph_endpoints) | HTTP endpoints: `definitions\|calls\|stats`. Filter by `path_contains`+`method`. |
| [`archigraph_endpoint_posture`](mcp-tools/cross-cutting-analysis.md#archigraph_endpoint_posture) | Endpoint posture: throws/catches + rate-limit + deprecation + feature-gates + auth. |
| [`archigraph_effective_contract`](mcp-tools/cross-cutting-analysis.md#archigraph_effective_contract) | Per-verb effective contract of a ViewSet/controller (or route). |
| [`archigraph_topology`](mcp-tools/cross-cutting-analysis.md#archigraph_topology) | Message-channel topology: orphan publishers/subscribers, topic detail. |
| [`archigraph_flows`](mcp-tools/cross-cutting-analysis.md#archigraph_flows) | Flow-process diagnostics: `dead_ends`, `truncated`, `detail`. |
| [`archigraph_graph_patterns`](mcp-tools/cross-cutting-analysis.md#archigraph_graph_patterns) | Indexer-extracted structural patterns (not agent store): `list\|get`. |
| [`archigraph_payload_drift`](mcp-tools/cross-cutting-analysis.md#archigraph_payload_drift) | Schema-drift findings on cross-repo HTTP endpoints (schema/envelope). |
| [`archigraph_cross_links`](mcp-tools/cross-cutting-analysis.md#archigraph_cross_links) | Cross-repo link candidates: `list=pending`, `accept\|reject=resolve`. |

### [Code behaviour & effects](mcp-tools/code-behaviour-and-effects.md)

Effects, control flow, purity, data-flow, and template literals.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_effects`](mcp-tools/code-behaviour-and-effects.md#archigraph_effects) | Effects + sinks; `include=branches\|effect_contexts`. |
| [`archigraph_control_flow`](mcp-tools/code-behaviour-and-effects.md#archigraph_control_flow) | On-demand per-function CFG + complexity; `detail=outline\|decisions\|data\|full`. |
| [`archigraph_pure_functions`](mcp-tools/code-behaviour-and-effects.md#archigraph_pure_functions) | Functions with no detected effects — memoization candidates. |
| [`archigraph_data_flows`](mcp-tools/code-behaviour-and-effects.md#archigraph_data_flows) | Request-input → sink DATA_FLOWS_TO edges (field/sink_kind/hop_path). |
| [`archigraph_def_use`](mcp-tools/code-behaviour-and-effects.md#archigraph_def_use) | Intra-procedural def-use chains (last-write-wins) per function. |
| [`archigraph_template_patterns`](mcp-tools/code-behaviour-and-effects.md#archigraph_template_patterns) | i18n / log_format / sql template literals lifted per file. |

### [Audit](mcp-tools/audit.md)

Security, secrets, licenses, test coverage, dead code, and cross-group parity.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_auth_coverage`](mcp-tools/audit.md#archigraph_auth_coverage) | Flag HTTP endpoints missing auth (severity, IDOR risk). |
| [`archigraph_secrets`](mcp-tools/audit.md#archigraph_secrets) | Scan for hardcoded secrets; masked findings by severity. |
| [`archigraph_license_audit`](mcp-tools/audit.md#archigraph_license_audit) | Audit dependency licenses; flag GPL/AGPL conflicts. |
| [`archigraph_test_coverage`](mcp-tools/audit.md#archigraph_test_coverage) | Production entities with no TESTS edge, ranked by severity. |
| [`archigraph_dead_code`](mcp-tools/audit.md#archigraph_dead_code) | Reachability dead-code: entities unreached by entry-points. |
| [`archigraph_find_dead_code`](mcp-tools/audit.md#archigraph_find_dead_code) | Dead/unwired code: isolated, marked-unused, or test-only symbols. |
| [`archigraph_security_findings`](mcp-tools/audit.md#archigraph_security_findings) | Taint-flow findings: source → sink paths ranked by confidence. |
| [`archigraph_contract_test_effectiveness`](mcp-tools/audit.md#archigraph_contract_test_effectiveness) | Tautological-spec detector: assertions that can never fail. |
| [`archigraph_literal_parity`](mcp-tools/audit.md#archigraph_literal_parity) | Cross-group ConstantSet/enum value-set parity diff. |
| [`archigraph_auth_posture_diff`](mcp-tools/audit.md#archigraph_auth_posture_diff) | Cross-group auth-posture parity diff per linked endpoint. |
| [`archigraph_stub_detector`](mcp-tools/audit.md#archigraph_stub_detector) | Cross-group stub detector: v3 pure where oracle computes. |
| [`archigraph_response_shape_diff`](mcp-tools/audit.md#archigraph_response_shape_diff) | Cross-group branch-aware response-shape parity diff per endpoint. |

### [Findings & docs](mcp-tools/findings-and-docs.md)

Memory store, agent pattern store, and the docgen staging workflow.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_save_finding`](mcp-tools/findings-and-docs.md#archigraph_save_finding) | Persist a Q&A pair to the group memory store. |
| [`archigraph_list_findings`](mcp-tools/findings-and-docs.md#archigraph_list_findings) | Read back saved findings. |
| [`archigraph_patterns`](mcp-tools/findings-and-docs.md#archigraph_patterns) | Agent pattern store: `query=find by task`, `record=store with exemplars`. |
| [`archigraph_docgen_start_run`](mcp-tools/findings-and-docs.md#archigraph_docgen_start_run) | Start or resume a local-staging docgen run. Returns `run_id` + `staging_path`. |
| [`archigraph_docgen_status`](mcp-tools/findings-and-docs.md#archigraph_docgen_status) | Inspect an in-flight docgen run: files written + SHA-256 per file. |
| [`archigraph_docgen_validate`](mcp-tools/findings-and-docs.md#archigraph_docgen_validate) | Lint frontmatter + cross-links. Read-only. |
| [`archigraph_docgen_promote`](mcp-tools/findings-and-docs.md#archigraph_docgen_promote) | Atomic staging → canonical rename. Blocks SSG scaffolding. |
| [`archigraph_docgen_abort`](mcp-tools/findings-and-docs.md#archigraph_docgen_abort) | Cancel a staging run: rm -rf staging, release per-group lock. |
| [`archigraph_docgen_list`](mcp-tools/findings-and-docs.md#archigraph_docgen_list) | List canonical doc files under `~/.archigraph/docs/<group>/`. |
| [`archigraph_apply_docgen_repairs`](mcp-tools/findings-and-docs.md#archigraph_apply_docgen_repairs) | Docgen feedback: apply repair candidates to graph enrichments. |
| [`archigraph_apply_doc_semantics`](mcp-tools/findings-and-docs.md#archigraph_apply_doc_semantics) | Doc L2: apply agent-produced DesignDecision nodes + RATIONALE_FOR edges. |

### [Admin & repair](mcp-tools/admin-and-repair.md)

Enrichment/repair queues, metrics, and local-only telemetry events.

| Tool | One-line purpose |
|------|-----------------|
| [`archigraph_enrichments`](mcp-tools/admin-and-repair.md#archigraph_enrichments) | Enrichment candidates: `list=pending`, `submit=resolve`, `reject=discard`. |
| [`archigraph_repairs`](mcp-tools/admin-and-repair.md#archigraph_repairs) | Residual-edge repair queue: `list=pending`, `submit=resolve`. |
| [`archigraph_mcp_metrics`](mcp-tools/admin-and-repair.md#archigraph_mcp_metrics) | Session tool-call metrics (counts, p50/p95 ms) + last N days rollups. |
| [`archigraph_persona_event`](mcp-tools/admin-and-repair.md#archigraph_persona_event) | Record persona lifecycle events. **LOCAL ONLY.** |
| [`archigraph_feedback_event`](mcp-tools/admin-and-repair.md#archigraph_feedback_event) | Record agent-experience feedback for a test run. **LOCAL ONLY.** |

---

## Sentinel tool

`archigraph_status` is registered as a real callable tool but is shown **only** when the agent's `cwd` falls outside all registered groups. It returns guidance on how to configure a group, and does not appear in the normal tool handshake for indexed sessions.

---

## Pairing with grep

archigraph MCP and grep are complementary. Use MCP for structural questions (who calls X, trace a flow, find callers). Use grep for raw enumeration (every `if err != nil`, every import line). See [CLAUDE.md](../CLAUDE.md) for the pairing guide with worked examples.
