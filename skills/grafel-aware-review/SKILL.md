---
name: grafel-aware-review
description: >
  Teaches an AI agent to use grafel MCP tools as a structural second pair
  of eyes during code review. Covers impact radius analysis, orphan detection,
  dead-end flow discovery, cross-repo consistency checks, and pattern
  violation spotting — with a per-change-type tool sequence, a severity
  rubric, and worked examples.
type: behavior
when-to-use: >
  Invoke when you are reviewing a PR diff and the grafel daemon is running
  for the affected group. Use for any change that touches function signatures,
  HTTP endpoints, inter-module wiring, or cross-repo calls. Do NOT invoke for
  pure style changes, comment edits, test-data fixtures, or single-file
  refactors that provably cannot propagate.
---

# grafel-aware-review

A structural review protocol for AI agents. This skill turns grafel MCP
into a systematic second opinion on every PR — one that catches impact leakage,
broken call chains, and pattern drift that diff-only review misses.

Prerequisite: the `/using-grafel` skill. This skill assumes you already
know all 19 MCP tools and the Pass-based orientation workflow.

---

## 1. Should I run grafel on this PR?

### Yes — run the structural checks when the diff touches:

| Change type | Key risk grafel catches |
|-------------|----------------------------|
| New/renamed function or method | Callers in other modules still expect the old signature |
| Deleted function or method | Orphan call-sites remain in any repo of the group |
| New HTTP endpoint | No client calls it yet (dead definition); or it clashes with an existing route |
| Changed HTTP endpoint path/method | Existing client calls become orphan callers |
| New inter-module import | Introduces an unexpected cluster coupling or a cycle |
| New message publish/subscribe | No subscriber (dead publish) or no publisher (dead subscriber) |
| Cross-repo call site added | Target entity may not exist in the other repo's graph |
| Pattern implementation (new handler, new serializer, etc.) | Does not follow the established structural pattern |

### No — skip grafel when the diff is limited to:

- Whitespace, formatting, or lint fixes.
- Comment or docstring edits.
- Test fixture data files (JSON, YAML, CSV).
- Changes that are entirely within a single private function with no exported signature.
- Config-only changes (`*.env`, `*.yaml`, `*.toml`) with no code entry points.
- Pure CSS/markup with no JS/TS wiring changes.

**Quick self-check:** if the change cannot be reached from any entity in the
grafel graph (no exported name, no route, no published event), skip the
structural pass and review the diff purely on its own terms.

---

## 2. Quick win — one call for 80 % of insights

When time is short, a single call surfaces most structural risk:

```
grafel_find(
  question="<paste the name of the changed entity>",
  depth=2,
  token_budget=800
)
```

This call returns:
- The entity's direct callers and callees (depth-1 neighbours).
- Their callers and callees (depth-2 neighbours).
- Whether any neighbours live in a different repo (cross-repo risk).
- PageRank of the changed entity (high PageRank = high blast radius).

If the entity is not found (`hits: []`), it is either brand-new (check for
orphan callers of the new name) or the repo has not been re-indexed since the
PR was opened.

**Estimated cost:** ~100–300 tokens. Run this first. If the result is clean
and the entity has low PageRank, you may not need the full per-change-type
sequence below.

---

## 3. Per-change-type MCP tool sequence

### 3.1 Function / method signature change

**Risk:** callers in other modules or repos still call the old signature,
or assume a return type that no longer exists.

```
# Step 1: Find the entity and all depth-2 callers
grafel_find(question="<FunctionName>", depth=2, context_filter=["CALLS"])

# Step 2: Inspect for PageRank (blast-radius indicator)
grafel_inspect(label_or_id="<FunctionName>")

# Step 3: For each top caller — does its call site match the new signature?
grafel_get_source(node_id="<CallerEntityId>", context_lines=20)

# Step 4: Check cross-repo callers
grafel_expand(node="<FunctionName>", depth=1)
# Look for entries where entity_id prefix differs from current repo slug
```

**Review comment trigger:** any caller found at depth 1 or 2 that is in a
different file or repo — confirm the call site is updated or explicitly listed
as a follow-up ticket.

---

### 3.2 Deleted function or method

**Risk:** call-sites anywhere in the group still reference the deleted name,
creating dead code (orphan callers).

```
# Step 1: Find all callers before deletion is confirmed as safe
grafel_find(question="<DeletedFunctionName>", depth=1)
# If hits is empty: the entity was already absent from the index — safe.
# If hits exist: callers remain.

# Step 2: Expand to enumerate every direct caller
grafel_expand(node="<DeletedFunctionName>", depth=1)

# Step 3: Source-read each caller to verify it is also removed in this PR
grafel_get_source(node_id="<CallerId>", context_lines=15)
```

**Review comment trigger:** any caller not removed in the same PR. Block
if caller is in a different repo (cross-repo breakage).

---

### 3.3 New HTTP endpoint

**Risk:** the endpoint is defined server-side but no client calls it
(dead definition), or it duplicates an existing route.

```
# Step 1: Confirm the new endpoint appears in the index (may need re-index)
grafel_endpoint_definitions(repo_filter=["<server-repo>"])
# Look for the new path+method pair in the returned list.

# Step 2: Check if any client call-site targets this path
grafel_endpoint_calls(repo_filter=["<client-repo>"])
# If none match: the endpoint is currently unused — note it.

# Step 3: Check for route conflicts (same path, any method)
grafel_find(question="<new-route-path>", depth=1)

# Step 4: Endpoint stats for overall orphan picture
grafel_endpoint_stats()
```

**Review comment trigger:** no matching client call found (potential dead
definition — note as intentional or request a follow-up). Route conflict
found (block).

---

### 3.4 Changed HTTP endpoint path or method

**Risk:** existing client call-sites become orphan callers pointing at
the old path.

```
# Step 1: Find orphan callers after the change
grafel_endpoint_calls(orphan_only=true)
# Any entry here references an endpoint that no longer has a definition.

# Step 2: Confirm the specific old path is now orphaned
grafel_find(question="<old-route-path>", depth=1)

# Step 3: Source-read each orphan caller to confirm it is updated in this PR
grafel_inspect(label_or_id="<orphan-caller-entity-id>")
grafel_get_source(node_id="<orphan-caller-entity-id>", context_lines=20)
```

**Review comment trigger:** any orphan caller that is NOT updated in this
PR. Block if the orphan caller is in a different repo.

---

### 3.5 New inter-module import or dependency

**Risk:** the new import creates an unexpected coupling between subsystems,
introduces a cluster-level cycle, or violates an established layering pattern.

```
# Step 1: Orient — which clusters do the importer and importee belong to?
grafel_clusters()
# Note cluster IDs for both the importing and imported entity.

# Step 2: Inspect both entities for cluster membership
grafel_inspect(label_or_id="<ImportingModule>")
grafel_inspect(label_or_id="<ImportedModule>")
# Compare `community_id` fields.

# Step 3: Check if a reverse path already exists (cycle detection)
grafel_trace(source="<ImportedModule>", target="<ImportingModule>")
# If found=true: this import creates a cycle — block.

# Step 4: Look for established layering patterns
grafel_list_findings(entity_id="<ImportingModule>")
# Prior agents may have recorded "this cluster must not depend on <other>".
```

**Review comment trigger:** cross-cluster import from a "lower" to
"higher" layer (invert layering). Cycle detected (block). New coupling
between two previously independent clusters (note as architectural decision).

---

### 3.6 New message publish or subscribe

**Risk:** a new publish has no subscriber in the group (dead publish), or
a new subscribe has no publisher (orphan subscriber).

```
# Step 1: Find the message topic entity
grafel_find(question="<TopicName OR event name>", depth=1,
                context_filter=["PUBLISHES_TO", "SUBSCRIBES_TO"])

# Step 2: Expand to see full publish/subscribe graph around the topic
grafel_expand(node="<TopicEntityId>", depth=2)
# Look for at least one PUBLISHES_TO and one SUBSCRIBES_TO edge.

# Step 3: Check cross-repo — publisher and subscriber may be in different repos
grafel_stats()  # verify both repos loaded without error
grafel_trace(source="<PublisherEntity>", target="<SubscriberEntity>")
```

**Review comment trigger:** publish with no subscriber (dead message —
note as intentional or request consumer). Subscribe with no publisher
(deadletter risk — block unless publisher is out-of-scope).

---

### 3.7 Cross-repo call site added

**Risk:** the target entity does not exist in the remote repo's graph, or the
cross-repo link candidate has not been confirmed.

```
# Step 1: Check link candidates for the new cross-repo edge
grafel_cross_links(action="list", repo_filter=["<calling-repo>"], limit=20)
# Find the new candidate corresponding to the added call.

# Step 2: Inspect the target entity in the remote repo
grafel_inspect(label_or_id="<remote-repo>::<TargetEntityId>")
# If not found: the target may not be indexed yet — note as risk.

# Step 3: Trace the end-to-end path
grafel_trace(source="<local-caller>", target="<remote-repo>::<target>")
# Verify crosses_repos=true and weakest_link_confidence is acceptable.
```

**Review comment trigger:** cross-repo link candidate not yet confirmed
(accept or reject it as part of review). Target entity not found in remote
graph (block — broken cross-repo reference until index confirms it).

---

### 3.8 Pattern implementation (new handler, serializer, migration, etc.)

**Risk:** the new entity does not follow the structural pattern established
for entities of this kind in this cluster.

```
# Step 1: Find an existing entity of the same kind for comparison
grafel_find(question="<existing example of same pattern>", depth=1)

# Step 2: Expand both the existing and new entity to compare neighbour sets
grafel_expand(node="<ExistingPatternEntity>", depth=1)
grafel_expand(node="<NewEntity>", depth=1)
# Compare edge kinds: both should have the same set (e.g., CALLS, REGISTERED_IN, USES).

# Step 3: Check saved pattern findings
grafel_list_findings(entity_id="<ExistingPatternEntity>")
# Look for findings of type="decision" that record the expected structure.

# Step 4: Discover patterns if none are saved
grafel_find(question="<pattern keyword> convention structure", depth=2)
```

**Review comment trigger:** missing edge that all existing entities of this
kind carry (e.g., new handler not registered in the router). New entity
in the wrong cluster (pattern placement violation).

---

## 4. Severity rubric

Use this rubric to decide how strongly to flag an grafel finding.

| Severity | Condition | Review action |
|----------|-----------|--------------|
| **Block** | Caller removed but call-site remains (orphan in same PR scope). Cross-repo breakage (orphan caller in another repo). Cycle introduced. Route conflict (duplicate path+method). | Request changes — do not approve until resolved. |
| **Warn** | New endpoint with no client call (may be intentional). Cross-cluster import that is not a cycle (architectural change). Cross-repo link candidate not yet confirmed. Dead publish with no subscriber in group. | Leave a comment asking for justification or a follow-up ticket. Do not block if author acknowledges. |
| **Note** | New entity with low PageRank and no cross-cluster callers. Pattern difference that is documented as intentional. | Inline comment only — informational. |
| **Pass** | Entity not found in index (brand-new, not yet indexed). Style-only diff. Pure test code with no exported names. | Skip grafel check — not applicable. |

**Confidence threshold:** any `grafel_trace` path with
`weakest_link_confidence < 0.5` should be verified with `grafel_get_source`
before escalating to Block. Low confidence edges may be indexer stubs, not
real edges.

---

## 5. Anti-patterns

### Do not run grafel on style-only PRs

A PR that only reformats code, renames local variables, or changes comments
produces zero useful grafel signal. Calling `grafel_find` on a
reformatted function name wastes tokens and produces noise. Apply the
self-check from Section 1 first.

### Do not skip Pass 0 even in review context

`grafel_whoami` + `grafel_stats` still costs ~200 tokens and prevents
querying the wrong group or a repo that failed to load. A repo that shows
`status: "load_error"` in stats means grafel results for that repo are
stale — lower confidence thresholds accordingly.

### Do not treat "entity not found" as "safe"

If `grafel_find` returns no hits for a changed entity, the most likely
explanation is the repo has not been re-indexed since the PR was opened —
NOT that the entity has no callers. Note the finding as "index may be stale"
rather than "no callers found."

### Do not use grafel to replace reading the diff

Grafel tells you about the structural graph — it does not know what
changed in the PR. Always read the diff first; use grafel to answer
follow-up structural questions the diff raises.

### Do not escalate low-confidence traces to Block without source verification

`grafel_trace` with `weakest_link_confidence < 0.5` means at least one
edge in the path is an indexer stub, not a confirmed call. Verify with
`grafel_get_source` before blocking a PR on the basis of that trace.

### Do not call grafel_expand at depth > 2 during review

Review bandwidth is limited. `grafel_expand` at depth 3 on a
high-PageRank entity returns hundreds of edges and is nearly impossible to
interpret in a review comment. Cap at depth 2; use `context_filter` to
narrow to the relevant edge kind.

---

## 6. Worked examples

Each example follows the same structure: PR summary → tool sequence → finding
→ review comment.

---

### Example A — Renamed public method, callers not updated

**PR summary:** `OrderService.create_order` renamed to
`OrderService.submit_order`. One file updated.

**Tool sequence:**

```
grafel_whoami()
# → group: "orders-platform", repo: "orders-api"

grafel_find(question="OrderService create_order", depth=2,
                context_filter=["CALLS"])
# → hits: [OrderService (orders-api), CheckoutController (orders-api),
#           MobileCheckoutFlow (mobile-app)]

grafel_inspect(label_or_id="CheckoutController")
# → source_file: src/controllers/checkout.py, community_id: 2

grafel_get_source(node_id="CheckoutController", context_lines=15)
# → line 88: self.order_service.create_order(cart)  ← old name, not updated

grafel_inspect(label_or_id="mobile-app::MobileCheckoutFlow")
# → cross-repo caller, not in this PR's diff
```

**Finding:** `CheckoutController` (same repo, not in diff) and
`MobileCheckoutFlow` (cross-repo) still call `create_order`.

**Severity:** Block (same-repo orphan caller). Block (cross-repo breakage).

**Review comment:**
> `grafel` finds two callers of the old `create_order` name not updated
> in this PR:
> - `src/controllers/checkout.py:88` (same repo) — still calls `create_order`.
> - `mobile-app::MobileCheckoutFlow` (cross-repo) — will break at runtime.
>
> Please update both before merging, or open a tracked follow-up for the
> cross-repo caller.

---

### Example B — New endpoint, no client calls it yet

**PR summary:** adds `POST /api/v2/invoices/bulk` server-side handler.

**Tool sequence:**

```
grafel_endpoint_definitions(repo_filter=["billing-api"])
# → definitions include: { method: "POST", path: "/api/v2/invoices/bulk", ... }

grafel_endpoint_calls(repo_filter=["admin-frontend"])
# → no call matching "/api/v2/invoices/bulk"

grafel_endpoint_calls(orphan_only=true)
# → 0 orphan callers (this is a new definition, not a changed one)

grafel_endpoint_stats()
# → definitions: 14, calls: 11, orphan_calls: 0
```

**Finding:** the new endpoint has no client call-site in any indexed repo.

**Severity:** Warn.

**Review comment:**
> `grafel` confirms the new `POST /api/v2/invoices/bulk` handler is
> registered, but no client call-site was found in the indexed group. If a
> client PR is forthcoming, please link it here. If this endpoint is
> intentionally internal-only or gated behind a feature flag, please note
> that in the PR description.

---

### Example C — Changed route path creates orphan callers

**PR summary:** renames `/api/v1/orders` to `/api/v2/orders` in the router.
Client not updated.

**Tool sequence:**

```
grafel_endpoint_calls(orphan_only=true)
# → [{ entity_id: "mobile-app::OrderListScreen", method: "GET",
#       path: "/api/v1/orders" }]

grafel_inspect(label_or_id="mobile-app::OrderListScreen")
# → source_file: src/screens/OrderList.tsx, repo: mobile-app

grafel_get_source(node_id="mobile-app::OrderListScreen", context_lines=10)
# → line 22: fetch("/api/v1/orders")  ← old path
```

**Finding:** `mobile-app::OrderListScreen` calls the old `/api/v1/orders`
path which is now orphaned.

**Severity:** Block (cross-repo orphan caller).

**Review comment:**
> `grafel` surfaces an orphan caller after the route rename:
> `mobile-app/src/screens/OrderList.tsx:22` still calls `GET /api/v1/orders`.
> This will 404 after merge. Please update the mobile-app call site in this
> PR, or revert the rename and use a migration strategy (keep v1 alias,
> add deprecation header, coordinate client update).

---

### Example D — New import introduces a layered dependency cycle

**PR summary:** `payments/gateway.py` imports from `reporting/metrics.py`.

**Tool sequence:**

```
grafel_whoami()
grafel_clusters()
# → cluster 1: payments (top entities: PaymentGateway, ChargeService)
# → cluster 4: reporting (top entities: MetricsDashboard, ReportingClient)

grafel_inspect(label_or_id="PaymentGateway")
# → community_id: 1

grafel_inspect(label_or_id="ReportingClient")
# → community_id: 4

grafel_trace(source="ReportingClient", target="PaymentGateway")
# → found: true, path: [ReportingClient → ... → PaymentGateway]
# → weakest_link_confidence: 0.92

# Now check the reverse (new import direction)
grafel_trace(source="PaymentGateway", target="ReportingClient")
# → found: true (via the new import in this PR)
# Together these confirm a cycle: payments ↔ reporting
```

**Finding:** the new import creates a bidirectional dependency between the
`payments` and `reporting` clusters.

**Severity:** Block (cycle detected, confirmed with source verification).

**Review comment:**
> `grafel` detects a dependency cycle introduced by this PR:
> `payments/gateway.py` now imports `reporting/metrics.py`, and
> `reporting` already depends on `payments` (via `ReportingClient →
> ... → PaymentGateway`, confidence 0.92). This creates a mutual cluster
> dependency that will make the modules impossible to import independently.
>
> Suggested fix: extract the shared metric-push logic into a new
> `core/instrumentation` module that neither cluster owns.

---

### Example E — New cross-repo call to unconfirmed target

**PR summary:** `admin-frontend` adds a fetch call to a new
`billing-api` endpoint `/api/v1/billing/summary` that was merged yesterday.

**Tool sequence:**

```
grafel_cross_links(action="list", repo_filter=["admin-frontend"], limit=10)
# → [{ candidate_id: "lc-a1b2c3", source: "admin-frontend::BillingWidget",
#       target: "billing-api::??", status: "pending" }]

grafel_inspect(label_or_id="billing-api::BillingSummaryHandler")
# → found, source_file: api/billing/views.py, community_id: 3

grafel_trace(source="admin-frontend::BillingWidget",
                 target="billing-api::BillingSummaryHandler")
# → found: true, crosses_repos: true, weakest_link_confidence: 0.70
```

**Finding:** the cross-repo link candidate is pending (not yet
confirmed). The trace exists but at the default 0.70 cross-repo confidence.

**Severity:** Warn (pending link candidate — should be accepted or rejected
as part of this review).

**Review comment:**
> `grafel` found a pending cross-repo link candidate (`lc-a1b2c3`)
> connecting `admin-frontend::BillingWidget` to `billing-api`.
> The trace resolves at 0.70 confidence (default cross-repo weight).
> Please confirm the cross-repo link is intentional by accepting the
> candidate (`grafel_cross_links(action="accept", candidate_id="lc-a1b2c3")`),
> or note it in the PR as a tracked follow-up.

---

### Example F — Pattern violation: new serializer missing registration

**PR summary:** adds `InvoiceItemSerializer` in `billing/serializers.py`
but does not register it in `billing/api.py`.

**Tool sequence:**

```
grafel_find(question="serializer billing", depth=1)
# → hits: [InvoiceSerializer, LineItemSerializer, InvoiceItemSerializer]

grafel_expand(node="InvoiceSerializer", depth=1)
# → edges: [CALLS → validate, REGISTERED_IN → billing/api.py::BillingRouter]

grafel_expand(node="InvoiceItemSerializer", depth=1)
# → edges: [CALLS → validate]
# ← missing REGISTERED_IN edge

grafel_list_findings(entity_id="InvoiceSerializer")
# → finding: "All billing serializers must be registered in BillingRouter
#              to be reachable from the API."
```

**Finding:** `InvoiceItemSerializer` is missing the `REGISTERED_IN` edge
that every other serializer in the cluster carries. A prior finding
documents this as a required pattern.

**Severity:** Block (established pattern violation, documented in findings).

**Review comment:**
> `grafel` shows `InvoiceItemSerializer` is missing the
> `REGISTERED_IN` edge to `billing/api.py::BillingRouter`. Every other
> serializer in the `billing` cluster (e.g., `InvoiceSerializer`,
> `LineItemSerializer`) has this edge. A saved finding records this
> as a required registration step. Please add the serializer to
> `BillingRouter` before merging.

---

## 7. Saving findings from a review

After completing a structural review, save any non-obvious decisions for
future agents:

```
grafel_save_finding(
  question="Why does payments cluster not import from reporting?",
  answer="Cycle prevention — see PR #<N>. Use core/instrumentation for
          shared metric-push. Enforced by cluster layering rule.",
  type="decision",
  nodes=["<PaymentGateway-id>", "<ReportingClient-id>"]
)
```

This takes ~50 tokens and means the next reviewer sees the rationale
immediately via `grafel_inspect` without re-running the trace.

---

## 8. Related skills

- `/using-grafel` — prerequisite. Full tool catalogue and Pass-based
  orientation workflow.
- `/grafel-quality-check` — use before a review marathon to verify the
  MCP is healthy on this group.
- `/grafel-repair` — if a review surfaces unresolved stubs that reduce
  trace confidence, run repair before re-reviewing.
- `/grafel-patterns-discover` — if Section 3.8 reveals no saved patterns,
  run patterns-discover to populate the finding store.

## 9. References

- `internal/mcp/SCHEMA.md` — canonical tool contract.
- ADR-0003 — SCOPE entity taxonomy (kind names, edge kinds).
- ADR-0008 — CWD-aware group routing.
- ADR-0009 — Cross-repo ID namespacing.
- ADR-0015 — Residual-edge repair flow.
- ADR-0018 — Agent-learned pattern store.
- ADR-0020 — Multi-branch + worktree graph snapshots. When reviewing a PR that
  is not yet merged, the branch's graph may already be indexed; use
  `grafel_diff(ref_a="main", ref_b="feature/...")` to get a structural
  summary before examining the line-level diff. See also
  [docs/user-guide/multi-branch.md](../../docs/user-guide/multi-branch.md).
- Issue #1269 — tracking issue for this skill.
