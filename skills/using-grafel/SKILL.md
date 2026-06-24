---
name: using-grafel
description: >
  Teaches an AI agent how to use grafel MCP tools effectively when working
  on a registered codebase. A task-oriented intent-to-tool map over the 22
  canonical tools, plus orientation workflow and hard anti-patterns.
type: behavior
when-to-use: >
  Invoke when you open a codebase that has grafel indexed, when you are
  about to navigate a large or unfamiliar codebase, or when you are asked a
  structural question (trace a flow, find callers, compare two endpoints,
  understand module layout) and the grafel daemon is running. Do NOT invoke
  for single-symbol lookups or for codebases that have never been indexed.
---

# using-grafel

A practical routing guide for AI agents in a grafel-registered codebase.
grafel exposes **22 intent-named tools**. Each one collapses a family of
related operations under a single verb with a discriminator parameter
(`kind` / `direction` / `aspect` / `detail` / `action` / `scope`). Pick the
tool by **intent**, then pick the discriminator value for the specific
question.

The single biggest win: **stop reasoning over raw source by hand.** When the
question is "are these two things different?", "what's affected?", "what
calls this?", or "what flows through here?", there is a dedicated tool that
answers it structurally. Reaching for `grafel_get_source` + manual reading is
the slow, error-prone path.

---

## 1. Intent → tool map

### Comparison & analysis (reach here FIRST)

| Your intent | Tool + discriminator |
|---|---|
| Compare reference vs candidate response shape | `grafel_diff` `aspect=response_shape` — **not** `get_source` + eyeball |
| Compare request payloads between two refs | `grafel_diff` `aspect=payload` |
| Compare auth posture between two refs | `grafel_diff` `aspect=auth` |
| Compare constant/literal parity between two refs | `grafel_diff` `aspect=literals` |
| Compare arbitrary entity sets between two refs | `grafel_diff` `aspect=refs` |
| What's affected if I change X (one entity) | `grafel_impact_radius` `scope=entity` |
| What's affected by this changeset / PR | `grafel_impact_radius` `scope=changeset` |
| Tech-debt: dead code | `grafel_debt` `kind=dead_code` |
| Tech-debt: import/dependency cycles | `grafel_debt` `kind=cycles` |
| Tech-debt: stub / unimplemented functions | `grafel_debt` `kind=stubs` |
| Tech-debt: impure functions | `grafel_debt` `kind=impure` |
| License audit | `grafel_debt` `kind=license` |
| Security findings | `grafel_security` `kind=findings` |
| Hardcoded secrets | `grafel_security` `kind=secrets` |
| Auth coverage across endpoints | `grafel_security` `kind=auth_coverage` |
| Test coverage | `grafel_test_analysis` `kind=coverage` |
| Test reachability | `grafel_test_analysis` `kind=reachability` |
| Contract-test effectiveness | `grafel_test_analysis` `kind=contract_effectiveness` |
| Recurring code patterns | `grafel_patterns` `kind=code` |
| Recurring graph patterns | `grafel_patterns` `kind=graph` |
| Template patterns | `grafel_patterns` `kind=template` |

### Navigation & traversal

| Your intent | Tool + discriminator |
|---|---|
| What calls X | `grafel_related` `direction=callers` |
| What X calls | `grafel_related` `direction=callees` |
| Direct neighbours of X | `grafel_related` `direction=neighbors` |
| What X uses | `grafel_related` `direction=uses` |
| What uses X | `grafel_related` `direction=used_by` |
| Route(s) between two entities | `grafel_find_paths` |
| Graph slice around entity(s) | `grafel_subgraph` |
| Trace data flow | `grafel_trace` `kind=data` |
| Trace control flow | `grafel_trace` `kind=control` |
| Trace def-use | `grafel_trace` `kind=def_use` |
| Trace effects | `grafel_trace` `kind=effects` |

### Locate & read

| Your intent | Tool |
|---|---|
| Find entities by name/pattern/kind | `grafel_find` |
| Full detail on one entity | `grafel_inspect` |
| Raw source for an entity/range | `grafel_get_source` |

### HTTP surface

| Your intent | Tool + discriminator |
|---|---|
| List HTTP endpoints | `grafel_endpoints` `detail=list` |
| Endpoint contract (request/response shape) | `grafel_endpoints` `detail=contract` |
| Endpoint security posture | `grafel_endpoints` `detail=posture` |
| Cross-repo client→server HTTP joins | `grafel_cross_links` |

### Orientation

| Your intent | Tool + discriminator |
|---|---|
| Where am I / which group+repo | `grafel_orient` `view=me` |
| Codebase overview | `grafel_orient` `view=overview` |
| Module clusters | `grafel_orient` `view=clusters` |
| Topology | `grafel_orient` `view=topology` |
| Module breakdown | `grafel_orient` `view=modules` |
| Per-repo freshness / is it indexed | `grafel_index_status` |

### Findings & workflow

| Your intent | Tool + discriminator |
|---|---|
| List saved findings | `grafel_findings` `action=list` |
| Save a finding | `grafel_findings` `action=save` |
| Run docgen lifecycle | `grafel_docgen` `action=start·status·list·promote·abort·validate` |
| Apply doc semantics/repairs/enrichments | `grafel_docgen_apply` `kind=semantics·repairs·enrichments` |

### Meta

| Your intent | Tool + discriminator |
|---|---|
| Emit feedback telemetry | `grafel_event` `kind=feedback` |
| Emit persona telemetry | `grafel_event` `kind=persona` |
| Tool-call metrics | `grafel_mcp_metrics` |

---

## 2. Should I use grafel at all?

Use grafel when the answer requires **graph traversal or structural
comparison** — relationships, call chains, flows, impact, diffs, posture.
Reach for grep/Read when you need **text search** — find a literal string,
read one known file, check a config value.

**Skip grafel** when:
- The question is answered by reading a single known file (use `Read`).
- You only need a literal string or regex (`rg`).
- The repo has never been indexed (check `grafel_index_status`).

---

## 3. Orientation (always run first)

Before deep work in an unfamiliar codebase, orient. This costs ~200 tokens
and prevents dozens of wasted traversals.

```
grafel_orient(view="me")         # which group + repo am I in?
grafel_orient(view="overview")   # size, repos, any unavailable
grafel_orient(view="clusters")   # major subsystems / module map
grafel_findings(action="list")   # prior session context
```

Exit when you know the group, the active repo, the approximate size, and the
top-level module names.

---

## 4. Worked examples

### "Are the reference and candidate endpoints in sync?"

```
# Don't get_source both and diff by hand — ask structurally:
grafel_diff(left="<ref>", right="<candidate>", aspect="response_shape")
grafel_diff(left="<ref>", right="<candidate>", aspect="payload")
grafel_diff(left="<ref>", right="<candidate>", aspect="auth")
```

### "What's affected if I change PaymentService.charge?"

```
grafel_impact_radius(target="PaymentService.charge", scope="entity")
```

### "Where is this HTTP endpoint implemented, and is it authed?"

```
grafel_endpoints(detail="list")                      # find the route
grafel_find(query="POST /api/v1/orders create order")
grafel_inspect(entity="createOrder")
grafel_endpoints(detail="posture")                   # auth/posture per endpoint
```

### "Trace a process flow end to end"

```
grafel_trace(target="CheckoutController.submit", kind="control")
grafel_trace(target="CheckoutController.submit", kind="data")
```

### "Find all callers of a function"

```
grafel_related(entity="PaymentService.charge", direction="callers")
```

### "Map cross-repo dependencies"

```
grafel_cross_links(group="orders-platform")
grafel_find_paths(from="mobile-app::UIComponent", to="api-backend::OrderService")
```

### "Audit code health"

```
grafel_debt(kind="dead_code")
grafel_debt(kind="cycles")
grafel_security(kind="findings")
grafel_test_analysis(kind="coverage")
```

---

## 5. Empty-result contract — never fabricate edges

**The top quality failure** in grafel-assisted analysis is confident
fabrication: inventing a plausible relationship when a traversal tool
returned zero edges.

When `grafel_related`, `grafel_subgraph`, or `grafel_trace` returns an empty
edge list for a **valid** entity, the response carries an explicit signal
(`result: "no_outgoing_edges"`, `"no_incoming_edges"`, `"no_edges"`, with a
human-readable `note`). These are distinct from `entity not found` errors
(`IsError: true`).

Required behaviour:
- If a traversal returns a no-edge `result`: **state that the graph shows no
  edge here.** Do not speculate or fill the gap from training data.
- Phrase it explicitly: _"The graph shows no callers for `create()`. No
  relationship was found."_
- Only after confirming no graph edge may you note the connection might exist
  but was not extracted — and mark it unverified.

```
# WRONG:
grafel_related(entity="orders-api::create", direction="callees")
→ { "result": "no_outgoing_edges", "note": "..." }
# Agent output: "create() calls the ORM save method"  ← FABRICATION

# RIGHT:
# "The graph shows no outgoing edges from create(). If an ORM save is
#  expected, this may be an extraction gap — verify with grafel_get_source
#  before asserting it."
```

---

## 6. Anti-patterns

### Don't reason over raw source when a tool answers it
Comparing two responses → `grafel_diff`, not `get_source` + manual diff.
Finding callers → `grafel_related direction=callers`, not reading every file.
Impact of a change → `grafel_impact_radius`, not tracing by hand.

### Don't use grafel for symbol lookup
```
# WRONG: grafel_find(query="PAYMENT_GATEWAY_URL")
# RIGHT: rg "PAYMENT_GATEWAY_URL" .
```

### Don't skip orientation
Jumping straight to `grafel_find` without `grafel_orient(view="me")` can mean
querying the wrong group or an unavailable repo.

### Don't use grafel for reading a known file
If you know the path, use `Read`. `grafel_get_source` is for when you only
know the entity label and want the graph to resolve the file.

### Don't over-expand
`grafel_subgraph` at high depth on a high-PageRank node can return thousands
of edges. Keep depth at 2 for routine use.

---

## 7. Reading responses

- **Bare ID** (`a1b2c3d4e5f60718`) — single-repo scope.
- **Prefixed ID** (`orders-api::a1b2c3d4e5f60718`) — multi-repo; preserve the
  prefix when passing it back.
- Entity kinds are returned without the `SCOPE.` prefix (`Component`,
  `Operation`, `Schema`, `Queue`, …).
- `grafel_find_paths` returns `weakest_link_confidence`; a path < 0.5 should
  be verified with `grafel_get_source`.
- `grafel_inspect` auto-attaches saved findings that reference the entity —
  read them before querying further.

---

## 8. Scoping rules

| Scenario | How to scope |
|---|---|
| Single repo | `repo_filter=["<repo-slug>"]` |
| Cross-repo | omit `repo_filter` (default = whole group) |
| Different group | `group="<group-name>"` |
| All repos explicitly | `repo_filter=["*"]` |

The daemon resolves your group from CWD by default. If `grafel_orient(view="me")`
returns `source: "none"`, provide `group=` explicitly or navigate to a
registered repo.

---

## 9. Related skills

- `/grafel-tech-docs` — full documentation pipeline that uses grafel at every pass.
- `/grafel-resolve` — residual-edge repair flow.
- `/grafel-graph-quality` — benchmarks grafel MCP against grep+read.
- `/grafel-patterns-discover` — finds and records structural patterns.
