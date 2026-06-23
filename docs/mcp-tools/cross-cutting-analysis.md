# MCP tools — Cross-cutting analysis

[← Back to the MCP tools index](../mcp-tools.md)

Modules, communities, HTTP surface, message topology, flows, and change impact.

---

## `grafel_clusters`

Louvain communities — a fast module map. When the group-algo overlay is applied, communities are computed over the assembled group graph (union of all repos + cross-repo links), so a community can span repos; otherwise it falls back to the per-repo communities baked into each `graph.fb`.

Key parameters: `repo_filter[]`, `top_entities_limit` (default 3), `min_size` (default 20).

Output: list of community clusters with representative entities. Group-scoped rows carry `repos[]` and a `cross_repo` flag; per-repo fallback rows carry a single `repo`.

---

## `grafel_module_analysis`

Module-level SCC + PageRank + betweenness.

Key parameters: `action` (`cycles`/`centrality`/`all`, default `all`).

Output: module SCCs (cycles), PageRank + betweenness centrality scores.

---

## `grafel_import_cycles`

IMPORTS cycle clusters per repo (Tarjan SCC).

Key parameters: `repo_filter[]`, `min_size` (default 2), `limit` (default 100).

Output: per-repo import cycle clusters.

---

## `grafel_quality_cycles`

Detect import cycles via Tarjan SCC, with the weakest edge and a fix hint.

Key parameters: `repo_filter[]`, `limit` (default 100).

Output: SCC lists representing circular import chains, with the weakest edge identified and a suggested fix.

---

## `grafel_impact_radius`

Inbound blast-radius: affected entities with `risk_score [0,1]`.

Key parameters: `entity_id` (required), `hops` (default 2).

Output: list of affected entities with `risk_score [0,1]` (higher = more transitive dependents).

---

## `grafel_pr_impact`

PR impact + merge-risk: maps changes → communities → blast radius.

Key parameters: `repo` (required), `base`, `head`, `refs[]`, `hops` (default 3).

Output: changed entities, the communities they touch, and the resulting blast radius.

---

## `grafel_diff_refs`

Diff two indexed git refs.

Key parameters: `repo` (required), `ref_a` (required), `ref_b` (required).

Output: added/removed/modified entities and relationships between the two indexed refs.

---

## `grafel_endpoints`

HTTP endpoints surface.

Key parameters: `action` (required: `definitions`/`calls`/`stats`), `path_contains`, `method`, `orphan_only`, `limit` (default 20), `offset` (default 0), `token_budget` (default 800), `format`, `kind`, `effect`, `include_navigation`.

Filters (`path_contains`, `method`) are applied **before** `limit`.

**Navigation surface.** Two params fold in-app NAVIGATES_TO routes into the same tool surface:

- `kind="navigation"` — short-circuits any `action` and returns aggregated navigation routes only. Each entry carries `route`, `to_id`, `call_sites`, `params_keys`, and a `sample_*` locator.
- `include_navigation=true` (with `action=definitions`) — preserves the HTTP-definitions payload and appends a `navigation_routes` array + `navigation_count`.

---

## `grafel_endpoint_posture`

Endpoint posture: throws/catches + rate-limit + deprecation + feature-gates + auth.

Key parameters: `entity_id`, `facet`, `path_contains`, `method`, `repo_filter[]`.

Output: per-endpoint posture facets.

---

## `grafel_effective_contract`

Per-verb effective contract of a ViewSet/controller (or route).

Key parameters: `entity_id` (required), `qualified_name`, `repo_filter[]`.

Output: the resolved per-HTTP-verb contract for the controller/route.

---

## `grafel_topology`

Message-channel topology.

Key parameters: `action` (required: `orphan_publishers`/`orphan_subscribers`/`topic_detail`/`topics`), `topic_id`, `repo_filter[]`.

Output: orphan publishers/subscribers and topic detail.

---

## `grafel_flows`

Flow-process diagnostics.

Key parameters: `action` (required: `dead_ends`/`truncated`/`detail`/`list`), `process_id`, `repo_filter[]`.

Output: dead-end flows, truncated flows, and per-process detail.

---

## `grafel_graph_patterns`

Indexer-extracted structural patterns (not the agent store).

Key parameters: `action` (required: `list`/`get`), `pattern_id`, `needs_attention` (bool), `status`, `confidence_min`, `limit` (default 50), `repo_filter[]`.

Output: browse (`list`) or detail (`get`) of patterns extracted by the indexer. See [`grafel_patterns`](findings-and-docs.md#grafel_patterns) for the agent-learned store.

---

## `grafel_payload_drift`

Schema-drift findings on cross-repo HTTP endpoints.

Key parameters: `drift_class`.

Output: drift findings (schema/envelope) on linked cross-repo endpoints.

---

## `grafel_cross_links`

Cross-repo link candidates.

Key parameters: `action` (required: `list`/`accept`/`reject`).

Output: cross-repo HTTP/Kafka/WS link records with match confidence.
