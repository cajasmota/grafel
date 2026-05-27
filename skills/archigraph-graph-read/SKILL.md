---
name: archigraph-graph-read
description: Shared archigraph read protocol — status → inspect → expand. Compose into any persona that consults the graph.
---

# READ Protocol

## Step 1 — status
Call `archigraph_whoami` first. Confirms the group name and which repos are indexed. If no graph is loaded, ask the user to run `archigraph index <path>` first.

## Step 2 — inspect
For each entity of interest (a class, function, file path the user named):
- `archigraph_inspect entity_id=<id-or-path>` returns the entity + 1-hop neighbors with line-precise CALLS/called_by edges
- `calls[].line` = line in the inspected entity's body where the outbound call appears
- `called_by[].line` + `called_by[].context` = line and ~40-char snippet in the caller's body
- `discriminators[]` (#2666) — when the entity does `var === literal` comparisons (e.g. `checklistType === 2`), each row carries `{file_line, literal, other_side}` so you can jump straight to the comparison without scanning the body. Discriminator literals are also mixed into the BM25 doc terms, so `archigraph_find` queries like "checklistType 2" surface the enclosing entity.
- Use these to pinpoint call sites without calling `get_source`
- Look at the neighbors for ORIENTATION before drilling deeper

## Step 3 — expand
When you need to traverse:
- `archigraph_expand entity_id=<id> edge=CALLS direction=incoming` for callers
- `archigraph_expand entity_id=<id> edge=CALLS direction=outgoing` for callees
- `archigraph_find name=<substring>` if you don't have an id yet

Other useful read tools to layer in: `archigraph_traces` (process-flow BFS), `archigraph_cross_links` (HTTP/Kafka/WS cross-repo), `archigraph_clusters` (Louvain communities), `archigraph_module_analysis`, `archigraph_subgraph`.

## When the READ phase is enough
Many user questions resolve at Step 2 (inspect a single entity, read the neighbors). Don't over-traverse. Three rules:
1. STOP when the entities you've seen answer the user's question
2. Don't enumerate edges past 2-hops unless the user asked
3. Prefer `archigraph_subgraph` for "give me a bounded view"
