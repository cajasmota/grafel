# MCP tools

grafel exposes **22 MCP tools**, all prefixed `grafel_`. Each is intent-named: one verb per job, with a discriminator parameter (`view` / `direction` / `kind` / `aspect` / `detail` / `scope` / `action`) selecting the variant. The canonical source of truth for inputs, outputs, and response shapes is:

**[`internal/mcp/SCHEMA.md`](../internal/mcp/SCHEMA.md)**

This page is the orientation-level **index**: a task-oriented routing table (intent → tool) followed by one section per tool with purpose, when-to-use, and discriminator values. For exhaustive response-field lists read SCHEMA.md directly.

---

## Setup

After `grafel install`, the MCP server is registered automatically in your agent's config. The daemon registers one server per machine; multiple groups can be active simultaneously. The server uses stdio transport.

To verify the wiring:

```sh
grafel status    # shows MCP: connected / disconnected
```

For per-agent config details see [agent-hosts.md](agent-hosts.md).

---

## cwd resolution

All routing tools accept an optional `cwd` parameter. On **macOS (darwin) and Windows**, cwd matching is **case-insensitive** (APFS, HFS+, NTFS). On Linux it is case-sensitive. This means passing `/Users/me/Projects/MyRepo` or `/users/me/projects/myrepo` both resolve to the same group on macOS (#2545).

Most tools also accept the common routing arguments `group`, `cwd`, and `ref` (optional git ref). See [SCHEMA.md → Common arguments](../internal/mcp/SCHEMA.md) for the full shared-parameter list.

---

## Intent → tool

Start every session with `grafel_orient` (`view=me`) to confirm group/repo, then route by intent. The comparison/analysis cluster is listed first — it is where agents most often reason over raw source by hand instead of letting grafel answer.

**Compare & analyse**

| I want to… | Tool · discriminator |
|---|---|
| Compare two refs/versions of an endpoint or contract | `grafel_diff` (`aspect=response_shape·payload·auth·literals·refs`) |
| Find tech-debt: dead code, cycles, stubs, impure fns, license risk | `grafel_debt` (`kind=dead_code·cycles·stubs·impure·license`) |
| Audit security posture, secrets, or auth coverage | `grafel_security` (`kind=findings·secrets·auth_coverage`) |
| Check test coverage, reachability, or test effectiveness | `grafel_test_analysis` (`kind=coverage·reachability·contract_effectiveness`) |
| Surface recurring code/graph/template patterns | `grafel_patterns` (`kind=code·graph·template`) |
| Know the blast radius of changing X (one entity or a changeset) | `grafel_impact_radius` (`scope=entity·changeset`) |

**Orient & discover**

| I want to… | Tool · discriminator |
|---|---|
| See where I am + the lay of the codebase | `grafel_orient` (`view=overview·me·clusters·topology·modules`) |
| Check whether my repo's index is fresh | `grafel_index_status` |
| Locate entities by name/pattern/kind | `grafel_find` |
| Get full detail on one entity | `grafel_inspect` |
| Read the raw source for an entity/range | `grafel_get_source` |

**Navigate the graph**

| I want to… | Tool · discriminator |
|---|---|
| Find callers/callees/neighbors/uses of X | `grafel_related` (`direction=callers·callees·neighbors·uses·used_by`) |
| Find the route(s) between two entities | `grafel_find_paths` |
| Get a graph slice around an entity | `grafel_subgraph` |
| Trace a data/control/def-use/effect flow | `grafel_trace` (`kind=data·control·def_use·effects`) |

**HTTP surface & cross-repo**

| I want to… | Tool · discriminator |
|---|---|
| List endpoints, or get their contract/posture | `grafel_endpoints` (`detail=list·contract·posture`) |
| See cross-repo client→server HTTP joins | `grafel_cross_links` |

**Findings, docs & meta**

| I want to… | Tool · discriminator |
|---|---|
| List or save findings | `grafel_findings` (`action=list·save`) |
| Run the docgen pipeline | `grafel_docgen` (`action=start·status·list·promote·abort·validate`) |
| Apply doc enrichments/repairs | `grafel_docgen_apply` (`kind=semantics·repairs·enrichments`) |
| Emit feedback/persona telemetry | `grafel_event` (`kind=feedback·persona`) |
| Read tool-call metrics | `grafel_mcp_metrics` |

---

## Core — interactive everyday surface (13)

### `grafel_orient`

Zoom-out: where am I, and the lay of the codebase. **Call `view=me` first** at the start of every session to confirm group/repo/ref.

- `view=me` — resolve group/repo/ref for the caller's cwd; the response doubles as a routing teaching signal.
- `view=overview` — key entities, cross-cutting edges, and orientation questions to seed exploration.
- `view=clusters` — Louvain communities with top-ranked entities; group-scoped (can span repos) when the group-algo overlay is applied. A fast module map.
- `view=topology` — message-channel topology: orphan publishers/subscribers, topic detail.
- `view=modules` — module-level SCC + PageRank + betweenness centrality.

Common params: `repo_filter[]`, plus per-view limits (`top_entities`, `top_edges`, `min_size`, …).

### `grafel_index_status`

Per-repo index freshness gate. **Lightweight** — reads only the scheduler snapshot; does NOT load or assemble the group graph, so it is cheap to poll.

Each repo reports `state` ∈ `current` | `queued` | `indexing` | `dirty`, plus `indexed_ref` (last completed index) and `head_ref` (pending work target).

Key params: `repo` (case-insensitive substring or exact path), `group`.

**Gating rule:** gate on **your own** repo's `state == "current"` (and `indexed_ref == head_ref` where known) — not a process-wide flag, which blocks on *any* repo's indexing.

### `grafel_find`

Locate entities by name/pattern/kind. BM25-ranked graph query with optional BFS expansion — the primary discovery tool.

Key params: `query` (required), `kind` / `kind_filter`, `mode` (default `bfs`), `depth` (default 3), `token_budget` (default 800), `repo_filter[]`, `cross_repo` (default `false`), `full`, `include_noise`, `min_confidence`.

**Scope default:** without `repo_filter` or `cross_repo=true`, search is scoped to the cwd-resolved repo. Pass `cross_repo=true` to span the group. `min_score` defaults to `0.15`.

### `grafel_inspect`

Full detail on one entity by id, qualified name, or label. Returns the full record plus line-precise calls/called_by.

Key params: `entity_id` (required), `verbose`, `repo_filter[]`, `include_unresolved` (default `false`), `min_confidence`.

Output includes `calls[]` (outbound CALLS, line-precise), `called_by[]` (inbound callers), `discriminators[]` (when DISCRIMINATES_ON edges exist), and a `metadata` provenance block (`indexed_ref`, `indexed_sha`, `indexed_at`, `age_seconds`).

### `grafel_get_source`

Raw source lines for an entity or file range.

Key params: `entity_id` (required), `context_lines` (default 8), `from_line` + `to_line` (exact range, no cap).

Output: source text with start/end line numbers. Times out gracefully on large files.

### `grafel_related`

Entities related to X. One tool for every adjacency query — pick the `direction`:

- `direction=callers` — inbound callers, **ranked by call frequency**. If `entity_id` starts with `/` and matches no entity, it is treated as an in-app route literal (searches NAVIGATES_TO edges; response carries `resolved_as: "navigation_route"`).
- `direction=callees` — outbound callees.
- `direction=neighbors` — graph neighbors (both directions).
- `direction=uses` / `direction=used_by` — usage edges: `CALLS`, `USES`, `USES_HOOK`, `REFERENCES` and `NAVIGATES_TO` (outgoing for `uses`, incoming for `used_by`), with route/param filters and multi-hop flow (`mode=flow`). Each returned edge carries its `kind`. Containment (`CONTAINS`), module dependency (`IMPORTS`/`DEPENDS_ON`) and type hierarchy (`EXTENDS`/`IMPLEMENTS`) are deliberately excluded — see #6314.

Key params: `entity_id` (required), `direction`, `depth` (default 1), `token_budget` (default 800), `route`, `with_param`, `repo_filter[]`.

### `grafel_find_paths`

Route(s) between two entities — confidence-weighted shortest path (Dijkstra). The verb stays explicit because "paths" collides with URL/file paths.

Key params: `from` (required), `to` (required), `max_hops` (default 5).

### `grafel_subgraph`

Graph slice around one or more entities — nodes+edges within N hops.

Key params: `entity_id` / `entities` (required), `depth` (default 2), `format` (`raw`/`markdown`, default `raw`), `max_nodes`.

Output: nodes+edges JSON (`raw`) or a Markdown summary (`markdown`).

### `grafel_trace`

Trace a flow through code. Pick the `kind`:

- `kind=data` — request-input → sink DATA_FLOWS_TO edges (`field`, `sink_kind`, `hop_path`).
- `kind=control` — per-function control-flow graph + cyclomatic complexity (`detail=outline·decisions·data·full`).
- `kind=def_use` — intra-procedural def-use chains (last-write-wins) per function.
- `kind=effects` — effects + sinks for a function (`include=branches·effect_contexts`).

Also covers pre-computed process-flow traces (list / get / follow a flow from an entry point) and the confidence-weighted path between two nodes.

Key params: `target` (required), `kind`, plus per-kind params (`sink_kind`, `detail`, `include`, `process_id`, `entry_point_id`, `max_depth`), `repo_filter[]`.

### `grafel_endpoints`

HTTP endpoints + their contract/posture. Pick the `detail`:

- `detail=list` — the HTTP surface (`definitions`/`calls`/`stats`); filter by `path_contains` + `method`. `kind="navigation"` folds in-app NAVIGATES_TO routes into the same surface; `include_navigation=true` appends them to the definitions payload.
- `detail=contract` — per-verb effective contract of a ViewSet/controller (or route).
- `detail=posture` — throws/catches + rate-limit + deprecation + feature-gates + auth per endpoint.

Key params: `detail`, `entity_id` / `qualified_name`, `path_contains`, `method`, `orphan_only`, `limit` (default 20), `repo_filter[]`.

### `grafel_cross_links`

Cross-repo client→server HTTP joins (also Kafka/WS link records) with match confidence.

Key params: `action` (`list`/`accept`/`reject`), `group`, `endpoint`.

### `grafel_impact_radius`

What's affected by changing X. Pick the `scope`:

- `scope=entity` — inbound blast-radius from one entity: affected entities with `risk_score [0,1]` (higher = more transitive dependents).
- `scope=changeset` — changeset/PR impact + merge-risk: changes → communities → blast radius.

Key params: `scope`, `entity_id` (entity scope) or `repo` + `base`/`head`/`refs[]` (changeset scope), `hops` (default 2–3).

### `grafel_diff`

Compare two refs/versions. Pick the `aspect`:

- `aspect=refs` — diff two indexed git refs: added/removed/modified entities + relationships.
- `aspect=response_shape` — branch-aware response-shape parity diff per endpoint.
- `aspect=payload` — schema-drift findings on cross-repo HTTP endpoints (schema/envelope).
- `aspect=auth` — auth-posture parity diff per linked endpoint.
- `aspect=literals` — ConstantSet/enum value-set parity diff.

Key params: `left` / `right` (or `repo` + `ref_a` + `ref_b`), `aspect`, plus per-aspect params (`set`, `drift_class`, …).

---

## Analysis (5)

### `grafel_debt`

Tech-debt / code-health findings — code smells, dead code, cycles, stubs. Pick the `kind`:

- `kind=dead_code` — entities unreached by entry-points (reachability) and isolated/marked-unused/test-only symbols.
- `kind=cycles` — IMPORTS cycle clusters per repo (Tarjan SCC) with the weakest edge and a fix hint.
- `kind=stubs` — stub detector: functions pure where a comparison baseline computes.
- `kind=impure` — functions with no detected effects (memoization candidates) and their inverse.
- `kind=license` — audit dependency licenses; flag GPL/AGPL conflicts.

Key params: `kind`, `repo_filter[]`, `kind_filter`, `limit`, `min_confidence`.

### `grafel_security`

Security posture. Pick the `kind`:

- `kind=findings` — taint-flow findings: source → sink paths ranked by confidence.
- `kind=secrets` — scan for hardcoded secrets; masked findings by severity (fixtures/opt-out suppressed).
- `kind=auth_coverage` — flag HTTP endpoints missing auth (severity, IDOR risk).

Key params: `kind`, `repo_filter[]`, `severity`, `category`, `min_confidence`, `only_missing`, `limit`.

### `grafel_test_analysis`

Test coverage / reach / effectiveness. Pick the `kind`:

- `kind=coverage` — production entities with no TESTS edge, ranked by severity, plus a coverage-freshness block (whether any ingested LCOV/Cobertura/JaCoCo report is stale relative to the latest index).
- `kind=reachability` — static test-reachability over TESTS + CALLS: which functions/endpoints are reached by any test path, and the orphans with none (`untested_only=true` is the canonical orphan query). A `[reachable-but-0%-lines]` tag flags reached-but-uncovered entities.
- `kind=contract_effectiveness` — the reachability × line-coverage cross-product, plus the tautological-spec detector (assertions that can never fail). Classifies entities into quadrants and lists the candidate ineffective tests. Degrades honestly when no line coverage is ingested.

Key params: `kind`, `entity_id` (scoped), `repo_filter[]`, `untested_only`, `endpoints_only`, `ineffective_only`, `severity`, `limit`.

### `grafel_patterns`

Recurring code/graph/template patterns. Pick the `kind`:

- `kind=code` — the agent-learned pattern store (ADR-0018): `action=query` to find by task, `action=record` to store with exemplars.
- `kind=graph` — indexer-extracted structural patterns: `action=list`/`get`, filter by `needs_attention`/`status`/`confidence_min`.
- `kind=template` — i18n / log_format / sql template literals lifted per file.

Key params: `kind`, `action`, `text`/`pattern_id`, `category`, `repo_filter[]`, `limit`.

### `grafel_findings`

The findings store. Pick the `action`:

- `action=save` — persist a Q&A pair to the group memory store (`~/.grafel/findings/<group>/`). Params: `question`, `answer`, optional `type`, `nodes[]`.
- `action=list` — read back saved findings, optionally filtered by `since`, `entity_id`, `limit`.

---

## Workflow — docgen pipeline (2)

### `grafel_docgen`

Docgen run lifecycle. Standard flow: `start` → write files into `staging_path` → `validate` → `promote`; `abort` resets a failed run, `status` checks progress. Pick the `action`:

- `action=start` — start or resume a docgen staging run for a group. Returns `run_id` + `staging_path`.
- `action=status` — files written + SHA-256 per file for an in-flight run.
- `action=validate` — lint frontmatter + cross-links. Read-only.
- `action=promote` — atomic staging → canonical rename; blocks SSG scaffolding; rotates the previous canonical set.
- `action=abort` — `rm -rf` staging and release the per-group lock; canonical untouched.
- `action=list` — list canonical doc files under `~/.grafel/docs/<group>/`.

Key params: `action`, `group`, `run_id`, `resume`, `force`, `no_git`.

### `grafel_docgen_apply`

Apply doc enrichments/repairs to the graph in a single batch. Pick the `kind`:

- `kind=semantics` — apply agent-produced DesignDecision nodes + RATIONALE_FOR edges (Doc L2).
- `kind=repairs` — apply docgen repair candidates / residual-edge repairs to graph enrichments.
- `kind=enrichments` — apply enrichment candidates (`http_endpoint`/`process_flow`/`message_topic` annotations).

Key params: `kind`, `action` (`list`/`submit`/`reject` for the queues), `repo_filter[]`, `dry_run`, `candidate_id`, `value`.

---

## Meta (2)

### `grafel_event`

Emit feedback/persona telemetry. **LOCAL ONLY** — events land in `~/.grafel/events/`; no data leaves the machine. Pick the `kind`:

- `kind=persona` — record a persona lifecycle event (`event_type=invoke`/`consult_out`/`save_finding`). Call at session start and on each Consult-Out.
- `kind=feedback` — record agent-experience feedback for a test run (`outcome`, optional `phase`/`library`/`capability`/`note`).

Group-agnostic — no `cwd` routing needed.

### `grafel_mcp_metrics`

Tool-call metrics + rollups. Current-session per-tool `calls`, `errors`, `p50_ms`, `p95_ms`, plus up to N days of persisted daily rollups from `~/.grafel/metrics/mcp-YYYY-MM-DD.jsonl`.

Key params: `days` (default 3). Group-agnostic.

---

## Pairing with grep

grafel MCP and grep are complementary. Use MCP for structural questions (who calls X, trace a flow, compare two endpoints). Use grep for raw enumeration (every `if err != nil`, every import line). See [CLAUDE.md](../CLAUDE.md) for the pairing guide with worked examples.
