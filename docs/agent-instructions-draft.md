# Draft: MCP `instructions` content for the initialize handshake

This file holds the dense agent-facing guide that PR 2 (`docs/mcp-instructions`)
will wire into `internal/mcp/server.go` via `mcpsrv.WithInstructions(...)`. The
content is not exposed to end users directly — it is delivered by the MCP server
in the initialize response. Edit here; the PR that wires it will embed via
//go:embed or a const string.

---

# archigraph — Agent Usage Guide

## What this gives you

archigraph builds and maintains a code knowledge graph across multi-repo groups and
exposes it over MCP. Entities (functions, classes, routes, schemas, queues, etc.)
and their relationships are indexed from source via tree-sitter, stored as
mmap'd FlatBuffers on disk, and served with microsecond-latency reads. A single
long-lived daemon process serves all registered groups on the machine; you query it
through the `mcpServers.archigraph` entry already in your host's MCP config.

## When to use archigraph

- **Cross-file reference lookup** — "what calls `OrderSerializer`?" "where is this
  queue declared?" Use `archigraph_search` or `archigraph_related` instead of grep.
- **Multi-repo navigation** — entities from every repo in the group are indexed
  together; cross-repo links (HTTP routes ↔ fetch calls, queue producers ↔
  consumers) are surfaced as first-class edges.
- **Framework-aware extraction** — Django views/routes, DRF viewsets, React hooks,
  NestJS services, Go chi routes, Spring MVC, etc. are extracted with structural
  understanding, not just text matching.
- **Dependency tracing** — `archigraph_trace` finds the confidence-weighted shortest
  path between any two nodes, crossing repo boundaries.
- **Agent memory** — `archigraph_save_finding` / `archigraph_list_findings` give you
  a durable scratchpad tied to the graph, persistent across sessions.

**Position relative to grep/glob:** archigraph is for structure and semantics. Use
grep for raw text patterns, string literals, or content inside comments. Use
archigraph for "what entity is this?", "what calls it?", "what does it depend on?",
"how do these two repos talk?".

## Cost model — queries are essentially free

Measured on real client fixtures after the mmap + FlatBuffers cache chain
(PRs #637 #638 #639 #644 — Phase D lazy mmap cache):

| Metric | Value |
|--------|-------|
| archigraph work per query | ~1.8 µs |
| MCP transport overhead | ~100–200 µs |
| **End-to-end per query** | **~100–200 µs** |
| Allocations per query | 10 (was 18,805 before mmap) |
| Bytes allocated per query | 496 B (was 1.4 MB) |
| ReadEntity speedup vs old JSON path | **6,800×** on hot lookups |

50–100 archigraph queries fit comfortably inside the latency of a single LLM
sampling token. **Do not avoid queries to save time or tokens — the cost is
negligible.** Issue follow-up queries freely as you learn more from each result.

Daemon RSS at idle: 38–100 MB. Per-repo peak during index: 257–385 MB. A
3-repo concurrent group holds ~434 MB. For reference, the closest competing
tool uses 3–6× more RAM for the same entity count.

## Recommended query patterns

**Orientation (do this once per session):**
```
archigraph_whoami
```
Confirms which group and repo the daemon resolved for your CWD. Call it if you
are unsure which repos are in scope.

**Start narrow, then expand:**
```
archigraph_describe  label_or_id="OrderViewSet"
archigraph_related   node="<id from above>"  depth=2
archigraph_search    question="who calls OrderViewSet" mode=bfs depth=3
```
Don't ask for the whole graph at once. Read one entity, learn its ID, fan out.

**Cross-repo tracing:**
```
archigraph_trace  source="mobile-app::OrderSerializer"  target="api-backend::Order"
```
Returns the shortest confidence-weighted path, including cross-repo overlay edges.

**Recent changes:**
```
archigraph_recent_activity  since="2026-05-15T00:00:00Z"  limit=20
```
Orients you after pulling — see which entities were touched.

**Residual repair loop (v1.0 demo pattern):**
```
archigraph_list_residuals   repo="my-repo"  limit=50
archigraph_submit_repair    repo="my-repo"  edge_id="er:..."  resolution="bind_to_entity"  ...
```
Use when the graph has unresolved stubs (`bug-extractor` / `bug-resolver`
dispositions). Submit repairs; optionally run `archigraph rebuild` to confirm
the bug rate dropped.

**Cross-repo link review:**
```
archigraph_list_link_candidates   limit=10
archigraph_resolve_link_candidate  candidate_id="lc-..."  decision="accept"
```

**Save a finding for later:**
```
archigraph_save_finding  question="How does auth flow from mobile to backend?"
                         answer="..."  nodes=["mobile-app::aaaa","api-backend::bbbb"]
```

## Anti-patterns

- **Don't ask for the whole graph in one shot.** `archigraph_search` with
  `repo_filter=["*"]` and no query will be large and slow. Start with a
  specific entity or question.
- **Don't avoid queries to save tokens.** At 100–200 µs each, 50 queries is
  ~10 ms of wall time. The savings from batching or skipping are negligible;
  the exploration value is not.
- **Don't reimplement archigraph queries with grep + Read.** If you want to
  know who calls a function, `archigraph_related` answers in one round-trip.
  grep + file reads across a large repo takes many more tokens and is less
  accurate.
- **Don't query archigraph for things it doesn't index.** Runtime values,
  the content of comments, test fixture data, and log output are not in the
  graph. Use grep or file reads for those.
- **Don't fabricate entity IDs.** IDs are 16-char hex hashes; always obtain
  them from a prior tool response. Round-trip IDs faithfully — preserve the
  `<repo>::<localId>` prefix when one is present.

## Tool reference

There are 19 registered tools, all prefixed `archigraph_`:

| Tool | Purpose | When to call |
|------|---------|--------------|
| `archigraph_whoami` | Return the inferred group + repo for the caller session. | Once per session to orient; when routing feels wrong. |
| `archigraph_search` | BM25-ranked graph query, optionally BFS-expanded. | First call when you have a keyword or natural-language question. |
| `archigraph_describe` | Look up an entity by ID, qualified name, or label. | After search to get full entity detail including source location. |
| `archigraph_related` | BFS neighbours of a node out to a given depth. | Fan-out from a known entity; explore dependencies and callers. |
| `archigraph_trace` | Confidence-weighted shortest path between two nodes. | "How does A connect to B?" across any number of hops. |
| `archigraph_get_source` | Source-file snippet for a node, with context lines. | Read the actual code for a specific entity without reading the whole file. |
| `archigraph_list_clusters` | List Louvain communities across loaded graphs. | Understand module structure; orient in an unfamiliar codebase. |
| `archigraph_recent_activity` | Entities whose source files changed after a given time. | Orient after pulling; see what changed since last session. |
| `archigraph_save_finding` | Persist a Q/A pair to the group's memory directory. | Checkpoint conclusions you'll need in a future session. |
| `archigraph_list_findings` | Retrieve previously saved findings, filtered by entity or time. | Resume a prior investigation; retrieve saved conclusions. |
| `archigraph_list_link_candidates` | List pending cross-repo link candidates. | Review auto-detected cross-repo connections before accepting. |
| `archigraph_resolve_link_candidate` | Accept or reject a cross-repo link candidate. | After reviewing `list_link_candidates`. |
| `archigraph_list_enrichment_candidates` | List pending enrichment candidates per repo. | When the graph needs human/agent annotation for entity purpose or kind. |
| `archigraph_submit_enrichment` | Submit a resolution for a pending enrichment candidate. | After reviewing `list_enrichment_candidates`. |
| `archigraph_reject_enrichment` | Reject a pending enrichment candidate. | When a candidate is wrong or unanswerable. |
| `archigraph_list_residuals` | Paginate `repair_edge` candidates (unresolved stubs). | Start of a repair session; feed into `archigraph_submit_repair`. |
| `archigraph_submit_repair` | Validate and persist a proposed edge repair. | After deciding how to resolve a residual from `list_residuals`. |
| `archigraph_graph_stats` | Corpus-level entity / relationship / community counts. | Sanity-check corpus size; confirm a repo is indexed. |
| `archigraph_get_telemetry` | Server uptime, per-tool counters, reload counts. | Debugging daemon health or query performance. |

**Note:** SCHEMA.md in this repo currently documents 17 tools — `archigraph_list_residuals`
and `archigraph_submit_repair` are registered in `internal/mcp/server.go` (PR #632)
but not yet reflected in that document. The 19-tool count above matches the live server.

## How groups and routing work

One daemon serves all groups registered in `~/.archigraph/registry.json`. Every
tool call that touches graph data resolves a group via this cascade (ADR-0008):

1. Explicit `group` argument on the call.
2. Caller's `cwd`, walked upward to find a `.archigraph/group.json` marker.
3. Singleton fallback when only one group is registered.

**In practice you don't need to set `group` explicitly.** Your CWD resolves it.
Use `archigraph_whoami` to confirm. Only pass an explicit `group` when you
intentionally want to query a group that isn't your current CWD's group.

Cross-repo IDs use the `<repo>::<localId>` prefix when a response spans multiple
repos. Pass them back as-is in follow-up calls.

## Connection

This agent connects via the global MCP entry already configured by `archigraph install`:

- **Claude Code (macOS):** `~/Library/Application Support/Claude/settings.json` →
  `mcpServers.archigraph`
- **Claude Code (Linux):** `~/.config/claude/settings.json` → `mcpServers.archigraph`
- **Windsurf:** `~/.codeium/windsurf/mcp_config.json` → `mcpServers.archigraph`

There is nothing per-repo to configure. Opening any repo registered in
`~/.archigraph/registry.json` makes it queryable through the same connection.

If archigraph isn't responding, the user can run:
```sh
archigraph status          # check daemon + group health
archigraph doctor          # smoke-check install + tools
archigraph mcp serve       # run the MCP server manually to see stderr
```

## Limits and known gaps

- **HTTP endpoint extraction is mid-overhaul.** Cross-repo HTTP route ↔ fetch
  links may be sparse on JS/TS frontends until issues #651, #653, #657 land.
  Django/Flask/Spring/Express/FastAPI/Go-chi HTTP extraction is solid; client-side
  fetch synthesis is the active work.
- **Java REFERENCES emission has a known gap.** The same class of residuals fixed
  for Python (#650) has a Java analogue that hasn't landed yet.
- **This is v1.0-track, not feature-complete.** Check `gh issue list` for current
  gaps or run `archigraph quality audit-orphans <repo>` to see the bug rate for
  a specific repo.
- **SCHEMA.md is slightly stale** (17 vs 19 tools). The live server is authoritative;
  `archigraph_get_telemetry` reports the actual registered call counters.
