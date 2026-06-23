# MCP tools — Graph traversal

[← Back to the MCP tools index](../mcp-tools.md)

Walk edges, find paths, and follow flows between entities. `grafel_neighbors` and the explicit `grafel_expand` / `grafel_find_callers` / `grafel_find_callees` are all first-class — reach for whichever reads clearest (`find_callers` for inbound callers, `find_callees` for outbound callees).

---

## `grafel_neighbors`

Graph neighbors of an entity.

Key parameters: `entity_id` (required), `direction` (`in`/`out`/`both`, default `both`), `depth` (default 1), `token_budget` (default 800), `fields[]`.

Output: list of neighboring entities with edge kind + direction. The directional `find_callers`/`find_callees` are first-class equivalents for when the explicit name reads clearer.

---

## `grafel_expand`

Return the neighbors of `entity_id` out to a given depth. Equivalent to `grafel_neighbors`; first-class, kept for the explicit name.

Key parameters: `entity_id` (or deprecated `node`), `depth` (default 1), `token_budget` (default 800), `repo_filter[]`, `fields[]`.

---

## `grafel_find_callers`

Inbound callers of `entity_id` (equivalent to `grafel_neighbors(direction=in)`); first-class.

Results are **ranked by call frequency** (descending) within each hop level, then alphabetically. Frequency is summed from `Properties["count"]` on CALLS edges (or 1.0 per raw edge when count is absent).

Key parameters: `entity_id` (required), `depth` (default 1), `token_budget` (default 800).

**Route-literal resolution.** If `entity_id` starts with `/` AND does not match any entity by ID or name, the handler treats it as an in-app route literal: it searches NAVIGATES_TO edges whose `ToID == "route:<literal>"` (or whose `Properties["route"]` equals the literal) and returns the push-site callers directly. Each caller carries `file`, `line`, `route`, and `params_keys`. Response includes `resolved_as: "navigation_route"`.

---

## `grafel_find_callees`

Outbound callees of `entity_id` (equivalent to `grafel_neighbors(direction=out)`); first-class.

Key parameters: `entity_id` (required), `depth` (default 1), `token_budget` (default 800).

---

## `grafel_find_paths`

Shortest path between two entities with confidence.

Key parameters: `from` (required), `to` (required), `max_hops` (default 5).

Output: path nodes + edges with per-hop confidence.

---

## `grafel_trace`

Confidence-weighted shortest path (Dijkstra) between two nodes.

Key parameters: `source` (required), `target` (required), `repo_filter[]`.

Output: Dijkstra shortest path with confidence weights.

---

## `grafel_traces`

Pre-computed process-flow traces.

Key parameters: `action` (`list`/`get`/`follow`, default `list`), `process_id`, `entry_point_id`, `max_depth` (default 8), `limit` (default 10), `token_budget` (default 800).

Output: process-flow trace records; `follow` does cross-stack BFS from an entry point.

---

## `grafel_navigates`

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
