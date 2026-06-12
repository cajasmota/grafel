# MCP tools — Graph traversal

[← Back to the MCP tools index](../mcp-tools.md)

Walk edges, find paths, and follow flows between entities. For new code prefer `archigraph_neighbors` over the deprecated `expand` / `find_callers` / `find_callees` aliases.

---

## `archigraph_neighbors`

Graph neighbors of an entity.

Key parameters: `entity_id` (required), `direction` (`in`/`out`/`both`, default `both`), `depth` (default 1), `token_budget` (default 800), `fields[]`.

Output: list of neighboring entities with edge kind + direction. Supersedes `find_callers`/`find_callees`.

---

## `archigraph_expand`

*Deprecated alias* of `archigraph_neighbors`. Returns neighbors of `entity_id`.

Key parameters: `entity_id` (or deprecated `node`), `depth` (default 1), `token_budget` (default 800), `repo_filter[]`, `fields[]`.

> **Deprecation**: prefer `archigraph_neighbors` for new code.

---

## `archigraph_find_callers`

*Deprecated alias* of `archigraph_neighbors(direction=in)` — inbound callers.

Results are **ranked by call frequency** (descending) within each hop level, then alphabetically. Frequency is summed from `Properties["count"]` on CALLS edges (or 1.0 per raw edge when count is absent).

Key parameters: `entity_id` (required), `depth` (default 1), `token_budget` (default 800).

**Route-literal resolution.** If `entity_id` starts with `/` AND does not match any entity by ID or name, the handler treats it as an in-app route literal: it searches NAVIGATES_TO edges whose `ToID == "route:<literal>"` (or whose `Properties["route"]` equals the literal) and returns the push-site callers directly. Each caller carries `file`, `line`, `route`, and `params_keys`. Response includes `resolved_as: "navigation_route"`.

> **Deprecation**: prefer `archigraph_neighbors(direction=in)` for new code.

---

## `archigraph_find_callees`

*Deprecated alias* of `archigraph_neighbors(direction=out)` — outbound callees.

Key parameters: `entity_id` (required), `depth` (default 1), `token_budget` (default 800).

> **Deprecation**: prefer `archigraph_neighbors(direction=out)` for new code.

---

## `archigraph_find_paths`

Shortest path between two entities with confidence.

Key parameters: `from` (required), `to` (required), `max_hops` (default 5).

Output: path nodes + edges with per-hop confidence.

---

## `archigraph_trace`

Confidence-weighted shortest path (Dijkstra) between two nodes.

Key parameters: `source` (required), `target` (required), `repo_filter[]`.

Output: Dijkstra shortest path with confidence weights.

---

## `archigraph_traces`

Pre-computed process-flow traces.

Key parameters: `action` (`list`/`get`/`follow`, default `list`), `process_id`, `entry_point_id`, `max_depth` (default 8), `limit` (default 10), `token_budget` (default 800).

Output: process-flow trace records; `follow` does cross-stack BFS from an entry point.

---

## `archigraph_navigates`

NAVIGATES_TO edge query emitted by the JS/TS navigation extractor. Coverage spans:

- Expo Router / React Navigation: `router.push`, `router.replace`, `router.navigate`, `navigation.navigate`, `navigation.push`, `Linking.openURL`.
- react-router-dom v6+: direct-call navigators (`const navigate = useNavigate(); navigate('/path', {state})`) and JSX components (`<Link to>`, `<NavLink to>`, `<Navigate to>`, `<Redirect to>`).
- react-router-dom v5: `useHistory().push` / `.replace`.
- Next.js: `useRouter().push` / `.replace`, `<Link href>` from `next/link`.

Key parameters:

- `entity_id` — source (outgoing) or destination (incoming) entity, as `repo::id`.
- `route` — substring filter on the route property (case-insensitive contains).
- `with_param` — return only edges whose `params` list includes this key name.
- `direction` — `outgoing` (default) or `incoming`.
- `mode` — `list` (default) or `flow` (multi-hop BFS following NAVIGATES_TO chains).
- `max_depth` — BFS depth limit for `mode=flow` (default 5).
- `limit` — max edges returned (default 100).
- `repo_filter[]` — restrict to named repos.

Output: `{ count, total, truncated, mode, direction, edges[] }` where each edge carries `from_id`, `from_name`, `from_repo`, `to_id`, `route`, `params`, `line`, `source_file`, and (in flow mode) `hop`.
