# MCP tools — Status & discovery

[← Back to the MCP tools index](../mcp-tools.md)

Orientation, search, and single-entity lookup. **Call `archigraph_whoami` first** at the start of every session. Most tools accept the common routing arguments `group`, `cwd`, and `ref`; see [SCHEMA.md](../../internal/mcp/SCHEMA.md) for the canonical schema.

---

## `archigraph_whoami`

Infer group/repo/ref for the caller.

**When to call**: every session start, before any other graph call.

Key parameters: `cwd` (optional; inferred from shell), `group` (optional override), `ref` (optional git ref).

Output: `cwd_resolved_to`, `group`, `repo`, `indexed_ref`, `is_worktree`.

---

## `archigraph_stats`

Corpus-level metrics.

Key parameters: `group`/`cwd`, `repo_filter[]`, `breakdown` (`"unresolved_imports"` adds edge taxonomy).

Output: entity counts per kind, relationship counts, unresolved import breakdown when requested.

---

## `archigraph_orient`

Orientation analysis: surfaces the most important entities, cross-cutting edges, and a set of orientation questions to seed exploration.

Key parameters: `repo_filter[]`, `top_entities` (default 15), `top_edges` (default 15), `max_questions` (default 12).

Output: ranked key entities, cross-cutting edge list, and suggested orientation questions.

---

## `archigraph_search_entities`

Substring search over entity names; ranked matches with source locations.

Key parameters: `query` (required), `kind_filter`, `limit` (default 30), `include_noise` (bool), `repo_filter[]`, `fields[]`, `format`, `token_budget`.

Output: ranked list of matching entities with source file + line.

---

## `archigraph_find`

BM25 graph query with optional BFS expansion. Primary discovery tool.

Key parameters: `query` (required), `mode` (default `bfs`), `depth` (default 3), `token_budget` (default 800), `repo_filter[]`, `cross_repo` (bool, default `false`), `full` (bool), `include_noise` (bool), `context_filter[]`, `fields[]`, `min_confidence` (default 0).

**Scope default:** when neither `repo_filter` nor `cross_repo=true` is supplied, the search is scoped to the cwd-resolved repo. Pass `cross_repo=true` to span all repos in the group. If cwd cannot be resolved to a repo, all repos are searched as a fallback. `min_score` defaults to `0.15`.

Output: BM25-scored entities with BFS expansion. Tail trimmed below `min_score`.

---

## `archigraph_inspect`

Look up a single entity by id, qualified name, or label. Returns the full record plus line-precise calls/called_by.

Key parameters: `entity_id` (required; accepts id, qname, or label), `verbose` (bool), `repo_filter[]`, `fields[]`, `include_unresolved` (bool, default `false`), `include`, `min_confidence`.

Output: full entity record including all properties + attached findings, plus:

- `calls[]` — outbound CALLS edges with line-precise data (`{target, target_path, line, via}`). Unresolved edges are filtered by default; pass `include_unresolved: true` to include them.
- `called_by[]` — inbound CALLS edges (callers). Always present even when empty. Each entry: `{source, source_path, line, context}`.
- `discriminators[]` — present only when the entity has DISCRIMINATES_ON edges; each row points at the discriminating comparison site.
- `metadata` — index provenance block: `{indexed_ref, indexed_sha, indexed_at, age_seconds}`.

---

## `archigraph_get_source`

Return actual source lines for a node (id/qname/label).

Key parameters: `entity_id` (required), `context_lines` (default 8), `from_line` + `to_line` (exact range, no cap).

Output: source text with start/end line numbers. Times out gracefully on large files.

---

## `archigraph_subgraph`

Nodes+edges within N hops of an entity.

Key parameters: `entity_id` (required), `depth` (default 2), `format` (`raw`/`markdown`, default `raw`), `max_nodes`.

Output: nodes+edges JSON (`raw`) or a human-readable Markdown summary (`markdown`).
