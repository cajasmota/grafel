# Pass 1b — Repair-aware Q&A

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

Surfaces the residuals Pass 1a could not auto-resolve (see Pass 1a § "Classify each residual" for the narrowed auto-resolve scope). The user answers in plain language; the agent translates each answer into an `grafel_docgen_apply(kind="repairs", action=submit)` call.

This pass handles all three resolution kinds — `bind_to_entity`, `reclassify_as_external`, `reclassify_as_dynamic` — because the user's answer provides the disambiguation that Pass 1a lacked. When the user provides a binding target for `bind_to_entity`, use `grafel_find` to confirm the entity exists before submitting; do not fabricate ids.

This pass is **skipped** when `~/.grafel/groups/<group>/repair-questions.json` is missing, empty, or contains only entries already answered in a prior run (see "Repair history" below).

## Inputs

- `~/.grafel/groups/<group>/repair-questions.json` (from Pass 1a).
- `~/.grafel/groups/<group>/repair-history.json` (optional; written by this pass to remember prior answers across runs).
- An grafel MCP session.

## Outputs

- Side effects: one `grafel_docgen_apply(kind="repairs", action=submit)` call per answered question.
- `~/.grafel/groups/<group>/repair-history.json` — appended with this run's Q/A pairs (per-residual). Keyed by `residual_id`. On the next run, Pass 1a consults this file before re-asking.
- `~/.grafel/groups/<group>/repair-sweep.md` — appended with a "User-answered" section.

## Procedure

### Step 1 — Group questions

Group questions by `repo`, then within each repo by `from_entity.kind`. This keeps the user's attention on one slice of the system at a time and reduces context switching.

### Step 2 — Open the conversation

Surface the global frame **once** at the top of the pass:

> grafel found **N** runtime-resolved edges that static analysis cannot bind from source alone. They fall into three buckets:
>
> 1. Dynamic URLs (template-literal or env-derived) — usually `reclassify_as_dynamic`.
> 2. Cross-repo HTTP calls — usually `bind_to_entity` to the matching backend route.
> 3. Third-party library/SaaS calls — usually `reclassify_as_external`.
>
> I'll ask you about each. Your answers become repair annotations the graph remembers across re-indexes.

### Step 3 — Ask each question

For each question in `repair-questions.json`, present it in this shape:

> **<repo>** · <from_entity.kind> `<from_entity.name>` <relation> `<original_stub>`
>
> _<summary line from Pass 1a>_
>
> Likely resolutions:
> - **A.** Bind to a known backend route (e.g., `<api-backend>::ContractsViewSet`).
> - **B.** Reclassify as dynamic (tenant-prefixed runtime URL).
> - **C.** Reclassify as external (third-party API).
> - **D.** Abandon (this edge is noise; drop it).
>
> Which fits, and (if A) what's the target id? Anything that helps me write a good repair `reasoning` is welcome.

### Step 4 — Translate answer → submit

Translate the user's natural-language answer into the corresponding `grafel_docgen_apply(kind="repairs", action=submit, ...)` call. Required fields per `docs/specs/repair-trust-model.md`:

| Answer | resolution | Required extra fields |
|---|---|---|
| "Routes to `<X>`" with an entity id from `inventory.json` | `bind_to_entity` | `target_entity_id` |
| "It's a third-party API" | `reclassify_as_external` | `module` (e.g., `stripe`, `aws-sdk`) |
| "It's resolved at runtime" | `reclassify_as_dynamic` | `dynamic_reason` (verbatim user phrase, trimmed) |
| "Resolved cross-repo; new target is `<id>`" | `reclassify_as_resolved` | `new_target` |
| "Drop it" / "Noise" | `abandon` | `abandon_reason` |

Always set:
- `confidence` — the agent's read of how confident the user was (1.0 for explicit, 0.7 for hedged, 0.5 for "best guess"). The trust model accepts <0.5 but flags it under `suspicious`.
- `reasoning` — a single sentence that paraphrases the user's answer. This is what later readers see when they query the graph. Avoid quoting verbatim if the user was terse; expand into a self-contained sentence. Never empty (R7).
- `source` — `"generate-docs/pass-1b"`.

### Step 5 — Handle rejections

If `grafel_docgen_apply(kind="repairs", action=submit)` returns `rejected_reason`, do not silently retry. Surface the reason to the user in their own terms:

- `target_entity_not_found` → "That id doesn't exist in the graph for this group. Did you mean one of: <top-3 fuzzy matches from `inventory.json`>?"
- `self_loop_disallowed` → "That would point the edge back at itself. Was this a typo, or should we abandon the edge?"
- `contradicts_contains_hierarchy` → "That target is the enclosing scope of `<from>`. Static analysis already owns that relationship. Pick a sibling target or abandon."
- `invalid_module_identifier` → "Module names can't contain path separators or shell characters. What's the package name on its own?"

Re-ask, then re-submit.

### Step 6 — Record history

After every successful submit, append to `repair-history.json`:

```json
{
  "residual_id": "er:...",
  "answered_at": "<rfc3339>",
  "user_phrasing": "<verbatim>",
  "resolution": "...",
  "submitted_payload": { ... },
  "submit_response": { ... }
}
```

This is what makes "don't re-ask the user about the same residual" work: Pass 1a reads this file before classifying. If a residual's `residual_id` (or its template-shape match) appears here with a successful resolution, Pass 1a auto-applies the prior resolution.

### Step 7 — Append to repair-sweep.md

Add a section to the file Pass 1a started:

```markdown
## User-answered (Pass 1b)
- `er:abc...` `<from> CALLS <stub>` → reclassify_as_dynamic (\"tenant-prefixed runtime URL\").
- `er:def...` `<from> CALLS <stub>` → bind_to_entity `<api-backend>::ContractsViewSet`.

K of K answered. 0 left for next run.
```

## Reporting back to the orchestrator

Return a single line:

> `repair Q&A: <K> asked, <answered> applied, <skipped> skipped, <rejected> rejected.`

Continue to Pass 2 regardless of skip/reject counts; the doc-gen pipeline is robust to partial repair.

## Invariants

- One submit per question. Never bundle multiple residuals into one submit call.
- Never invent `target_entity_id` values the user did not provide. If the user gestures at a target without an exact id, search with `grafel_find` and present the candidates back to the user; only submit once they pick one.
- The pass is **interactive** — never silently fall through unanswered questions. If the user requests "skip the rest," record the remaining questions back into `repair-questions.json` for the next run.
- Don't re-ask answered residuals. The history file is authoritative.

---

**[pass-01b telemetry]** Print at end of this pass:
```
[pass-01b] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
