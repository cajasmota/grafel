---
name: grafel-graph-read
description: Shared grafel read protocol — status → inspect → expand. Compose into any persona that consults the graph.
---

# READ Protocol

## Step 1 — status
Call `grafel_whoami` first. Confirms the group name and which repos are indexed. If no graph is loaded, ask the user to run `grafel index <path>` first.

## Step 2 — inspect
For each entity of interest (a class, function, file path the user named):
- `grafel_inspect entity_id=<id-or-path>` returns the entity + 1-hop neighbors with line-precise CALLS/called_by edges
- `calls[].line` = line in the inspected entity's body where the outbound call appears
- `called_by[].line` + `called_by[].context` = line and ~40-char snippet in the caller's body
- `discriminators[]` (#2666) — when the entity does `var === literal` comparisons (e.g. `checklistType === 2`), each row carries `{file_line, literal, other_side}` so you can jump straight to the comparison without scanning the body. Discriminator literals are also mixed into the BM25 doc terms, so `grafel_find` queries like "checklistType 2" surface the enclosing entity.
- Use these to pinpoint call sites without calling `get_source`
- Look at the neighbors for ORIENTATION before drilling deeper

## Step 3 — expand
When you need to traverse:
- `grafel_expand entity_id=<id> edge=CALLS direction=incoming` for callers
- `grafel_expand entity_id=<id> edge=CALLS direction=outgoing` for callees
- `grafel_find name=<substring>` if you don't have an id yet

Other useful read tools to layer in: `grafel_traces` (process-flow BFS), `grafel_cross_links` (HTTP/Kafka/WS cross-repo), `grafel_clusters` (Louvain communities), `grafel_module_analysis`, `grafel_subgraph`.

### In-app navigation (#2665)
For Expo Router / React Navigation / Next.js push-sites, two shortcuts fold the NAVIGATES_TO graph into the existing read tools:
- `grafel_endpoints(kind="navigation")` — list distinct routes, with merged `params_keys` (sorted, deduped JSON array) per route. Use this to answer "which screens take param X?".
- `grafel_find_callers("/route/literal")` — pass a literal beginning with `/`; the handler reverse-traverses NAVIGATES_TO and returns push-site callers with `file`, `line`, and per-call `params_keys`. Use this when adding a required param: callers whose `params_keys` is missing the new key are the diff candidates.

## When the READ phase is enough
Many user questions resolve at Step 2 (inspect a single entity, read the neighbors). Don't over-traverse. Three rules:
1. STOP when the entities you've seen answer the user's question
2. Don't enumerate edges past 2-hops unless the user asked
3. Prefer `grafel_subgraph` for "give me a bounded view"
