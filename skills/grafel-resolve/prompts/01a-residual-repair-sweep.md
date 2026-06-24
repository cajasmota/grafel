# Pass 1a — Residual repair sweep (pre-Q&A)

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_subgraph`, `grafel_orient (view=overview)`,
`grafel_orient (view=clusters)`, `grafel_orient (view=me)`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_orient (view=me)` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

Runs **after Pass 1 (inventory)** and **before any deeper writer passes**. Closes the gap between the static-analysis recall ceiling (~85% on cross-repo b→a) and full coverage by annotating runtime-resolved edges that the resolver structurally cannot bind.

This pass is the doc skill's integration with the ADR-0015 residual-repair mechanism. The MCP tool `grafel_docgen_apply (kind=repairs)` is the only surface used; do **not** read or write `repair.json` directly. See `docs/specs/repair-trust-model.md` for the allowlist of resolutions and verification rules.

## When to skip

- If `grafel_docgen_apply(kind="repairs", action=list, limit=1)` returns `total == 0` for every repo in the group, write a single line into `~/.grafel/groups/<group>/repair-sweep.md` ("No residuals at sweep time.") and hand back to the orchestrator.
- If a previous run of this pass already produced `repair-sweep.md` for the current commit (track via `git rev-parse HEAD` of each repo recorded inside the file's frontmatter), do **not** re-ask the user about the same residuals. Only surface deltas. This implements the "repair history" bonus from #732.

## Inputs

- `~/.grafel/groups/<group>/domain.md` — Pass 0 output.
- `~/.grafel/groups/<group>/inventory.json` — Pass 1 output.
- Optional: `~/.grafel/groups/<group>/repair-templates.json` — saved repair templates from prior runs (see "Repair templates" below). Absent on first run.
- An grafel MCP session.

## Outputs

- `~/.grafel/groups/<group>/repair-sweep.md` — human-readable summary (counts per repo, auto-resolved sample, pending-for-user list).
- `~/.grafel/groups/<group>/repair-questions.json` — machine-readable list of residuals that need user input, consumed by Pass 1b (repair-aware Q&A).
- Side effects: zero or more `grafel_docgen_apply(kind="repairs", action=submit)` calls per auto-resolved residual.

## Procedure

### Step 1 — List residuals per repo

For each repo `<r>` in the group, page through:

```
grafel_docgen_apply(kind="repairs", action=list, repo_filter=["<r>"], limit=50, offset=0)
```

Continue paging while the returned `residuals[]` length equals `limit`. Cap total per-repo scan at 1,000 residuals for Phase 1 — if a repo has more, record the truncation in `repair-sweep.md` and surface the count to the user; the deferred residuals will roll forward to the next run.

### Step 2 — Classify each residual

For each residual returned, decide one of:

1. **Auto-resolve** — apply a repair without asking the user. Allowed only when:
   - A repair template (Step 4) matches the residual's shape with `confidence >= 0.8`, OR
   - The residual is a recognised third-party API call (e.g., `original_stub` starts with `https://api.<vendor>/` or matches a well-known SDK stub like `stripe.charges.create`) and the relation is `CALLS` → `reclassify_as_external` with `module=<vendor>`.
   - **DO NOT auto-resolve `bind_to_entity` here.** Pass 1 (inventory) produces a corpus census (top-5 entities per community, kind counts). It does not provide full entity-level data, so there is no reliable way to confirm that a single unambiguous binding target exists. All `bind_to_entity` candidates must go to bucket 2 (Ask user) or be deferred to Pass 3a (generation-time, where the writer has full subgraph context via `grafel_subgraph`).
2. **Ask user** — surface to Pass 1b. Use this for `bind_to_entity` candidates, dynamic baseURL / tenant-prefixed routes, and any runtime-resolved edge where the resolution is not mechanically deterministic.
3. **Defer** — skip for this run (record reason). Defer is only legal when residual carries no actionable context window (extremely rare; should not happen on real corpora).

Every auto-resolve decision must have a one-sentence `reasoning` string and a `confidence` value. The trust model (`R7`) requires non-empty reasoning. Aim for `confidence >= 0.7` on auto-resolves; lower confidence belongs in the user-question bucket.

### Step 3 — Submit auto-resolutions

For each residual classified as auto-resolve, call:

```
grafel_docgen_apply(kind="repairs", action=submit,
  residual_id="er:...",
  resolution=<bind_to_entity | reclassify_as_external | reclassify_as_dynamic | reclassify_as_resolved | abandon>,
  target_entity_id=...,      # if bind_to_entity
  module=...,                # if reclassify_as_external
  new_target=...,            # if reclassify_as_resolved
  dynamic_reason=...,        # if reclassify_as_dynamic
  abandon_reason=...,        # if abandon
  confidence=<0..1>,
  reasoning="<one sentence>",
  source="generate-docs/pass-1a")
```

Inspect the response. If `rejected_reason` is present (e.g., `target_entity_not_found`, `self_loop_disallowed`), demote the residual to the user-question bucket and record the rejection reason. The trust model rejections are non-recoverable from inside this pass — the agent must hand to the user.

### Step 4 — Repair templates (bonus from #732)

A **repair template** captures the *shape* of a residual + the *kind* of resolution that fits it, so future runs can auto-resolve similar residuals without asking the user.

Templates live in `~/.grafel/groups/<group>/repair-templates.json`:

```json
{
  "version": 1,
  "templates": [
    {
      "id": "tmpl-dyn-tenant-prefix",
      "matches": {
        "relation": "CALLS",
        "original_stub_regex": "^/\\$\\{tenantId\\}/.*",
        "from_kind": "Component"
      },
      "resolution": "reclassify_as_dynamic",
      "dynamic_reason": "Tenant-prefixed runtime URL; resolves per-tenant at request time.",
      "confidence": 0.85,
      "applied_count": 11,
      "promoted_at": "2026-04-18T..."
    }
  ]
}
```

A template is **promoted** automatically when the same shape (same `relation` + same `original_stub` regex bucket + same `from_kind`) is auto-resolved successfully `>= 3` times in one sweep, OR when the user answers `>= 3` Pass 1b questions with the same resolution + reasoning. This is the "repair confidence model" bonus — after N successful applies of the same resolution shape, the agent applies it silently on subsequent matches.

Templates are advisory; the indexer's trust-model rules in `docs/specs/repair-trust-model.md` still gate every submit.

### Step 5 — Hand-off to Pass 1b

Anything classified as **ask user** is written to `repair-questions.json`:

```json
{
  "group": "<group>",
  "generated_at": "<rfc3339>",
  "questions": [
    {
      "residual_id": "er:deadbeef00000001",
      "repo": "mobile-app",
      "relation": "CALLS",
      "original_stub": "/${tenantId}/contracts",
      "from_entity": { "id": "...", "name": "DashboardScreen", "kind": "Component" },
      "summary": "Frontend Component DashboardScreen calls /${tenantId}/contracts at runtime.",
      "candidate_resolutions": [
        { "resolution": "reclassify_as_dynamic", "hint": "tenant-prefixed route" },
        { "resolution": "bind_to_entity", "hint": "<api-backend>::ContractsViewSet" }
      ]
    }
  ]
}
```

Pass 1b reads this file directly. Do **not** include auto-resolved residuals in `questions[]` (those are already done).

## Output — repair-sweep.md

```markdown
---
group: <group>
generated_at: <rfc3339>
heads:
  <repo-a>: <commit sha>
  <repo-b>: <commit sha>
---

# Residual repair sweep

grafel surfaced **N** residual edges across the group. Of those:

- **M** auto-resolved by this pass (see `repair-sweep-applied.json` for the full list).
- **K** will be asked of you in Pass 1b (Q&A).
- **D** deferred to a future run (reason recorded).

## Per-repo breakdown
| repo | total | auto | ask | defer |
|------|------:|-----:|----:|------:|
| <r>  | ...   | ...  | ... | ...   |

## Templates active this run
- `tmpl-dyn-tenant-prefix` — applied 11 ×.
- `tmpl-react-npm-allowlist` — applied 5 ×.

## Sample auto-resolutions
- `er:abc...` `Component DashboardScreen` `CALLS` `/${tenantId}/contracts` → reclassify_as_dynamic ("tenant-prefixed runtime URL").
- `er:def...` `Component CheckoutForm` `CALLS` `stripe.charges.create` → reclassify_as_external (`module=stripe`).

## Next step
Run Pass 1b to answer the K residual questions in `repair-questions.json`.
```

## Reporting back to the orchestrator

Return a single line in the form:

> `repair sweep: <total> residuals — auto-resolved <M>, asking user <K>, deferred <D>.`

If any repo could not be scanned (MCP error, missing `.grafel/enrichment-candidates.json`), record it in `repair-sweep.md` under a "Scan errors" section and continue with the repos that succeeded. Do **not** abort the doc-gen pipeline; repair is additive.

## Cross-link to patterns (bonus from #732)

Before recording a `PatternCandidate` in Pass 4 / aggregating in Pass 10, the agent checks whether the exemplar entity has any unresolved outbound edges via `grafel_docgen_apply(kind="repairs", action=list, repo_filter=[<r>])`. If so, the pattern record flow attempts a repair (Step 3 procedure) before storing the exemplar. This keeps pattern exemplars from referencing dangling targets.

## Invariants

- Never write to `repair.json` directly. The MCP tool is the only surface.
- Never invent a `target_entity_id` that doesn't appear in `inventory.json`. Trust-model rule `R2` rejects unknown targets; the rejection becomes a user question.
- Never submit `reasoning` shorter than ~10 characters. Trust-model rule `R7` flags it; we treat <10 as effectively rejected and demote to user question.
- Auto-resolution must be reproducible — the same `repair-templates.json` + same `inventory.json` + same residuals must produce the same submits (no time-based or random decisions). This preserves ADR-0015's determinism guarantee.

---

**[pass-01a telemetry]** Print at end of this pass:
```
[pass-01a] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
