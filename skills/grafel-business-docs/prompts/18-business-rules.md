# Pass 18 — Business rules / requirements (BUSINESS tier)

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

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

Reverse-engineer the product's business rules — constraints, requirements,
policies — from the implementation, and state them as PRODUCT REQUIREMENTS. This
is the layer a team lead extracts engineering requirements from; here we
generate it backwards (code → requirement). This is the inverse of the normal
workflow and is the owner's headline reason for the business tier.

> **READ FIRST:** `snippets/business-voice.md`. Binding. A rule reads like a
> requirement, not like code:
>   GOOD: "An inspection cannot be submitted until every device on the checklist
>          has an outcome recorded."
>   BAD:  "`InspectionSerializer.validate()` raises when `devices` is empty."

Synthesised across the whole group.

## Inputs

- `~/.grafel/docs/<group>/business/domain-glossary.md` (Pass 15).
- `~/.grafel/docs/<group>/business/capabilities/*.md` (Pass 16) — rules attach to
  capabilities.
- The graph: validation logic, conditional branches, permission checks,
  required fields.

## Output

```
~/.grafel/docs/<group>/business/rules/index.md   # grouped by business area
```

Use `output-templates/business-rules.md`. One file is usually enough; split into
`rules/<area>.md` only if a single area has many rules and the index grows
unwieldy. If you split, `rules/index.md` must still exist and link to each area
file (link-hygiene: no bare-directory links).

## Procedure

### Step 1 — Mine the implementation for rules

```
grafel_find(question="validation rules and required fields", repo_filter=null, depth=2, token_budget=1500)
grafel_find(question="permission and authorization checks — who can do what", repo_filter=null, depth=2, token_budget=1500)
grafel_find(question="state transitions and status constraints", repo_filter=null, depth=2, token_budget=1200)
grafel_find(question="business invariants and conditional logic that rejects input", repo_filter=null, depth=2, token_budget=1200)
```

Also fold in any latent-bug / security findings the run produced (e.g. an
endpoint missing a permission check) — surface those as a rule that SHOULD hold
plus a flagged gap, in business terms ("Only authorised staff should be able to
clear inspections; today this is not enforced for one action — flagged for
engineering").

### Step 2 — Translate each into a requirement

Each rule: the requirement (one sentence), why it exists, when it applies, and
what the product does if violated — all in business language. Group by business
area (Inspections, Access & permissions, Reporting, …). Attach rules to
capabilities by linking back to `capabilities/<slug>.md`.

Discard pure-technical guards (null checks, type coercions) — they are not
business rules.

### Step 3 — Anchors + provenance

Headings first, derive `anchors:` per `snippets/anchor-contract.md`. The
capability pages forward-link to specific rule anchors, so name your rule
headings clearly and stably. File/symbol references ONLY in `<details>`.

### Step 4 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. This pass's
`grafel_find` queries on validation, permission checks, and state
transitions produce graph data that may surface repair candidates — specifically:

- **`add_edge`** — permission checks often call a shared auth/permission
  utility whose edge to the validation logic is dynamic (decorator-based,
  mixin-based). If you can identify the concrete callee from source context
  while mining validation rules, emit an `add_edge`.

  Example:

  ```json
  {
    "type": "add_edge",
    "source_entity_id": "<InspectionSubmitView entity id>",
    "target": "IsInspectorPermission",
    "edge_kind": "CALLS",
    "confidence": 0.80,
    "evidence": "inspections/views.py@line 14: permission_classes = [IsInspectorPermission] — decorator permission not captured as a CALLS edge in graph",
    "source": "generate-docs/pass-18",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`fix_kind`** — state-machine queries sometimes surface entities that are
  mis-classified (e.g. a status enum catalogued as `Function`).

Only emit when `grafel_find` returned an entity that you then inspected
closely enough to have concrete file-level evidence. Business rule translation
itself is not evidence.

Use `source: "generate-docs/pass-18"` in all candidates. Append to
`~/.grafel/groups/<group>/docgen-repairs.jsonl`.

### Step 5 — Verify + save

Run `snippets/verification-checklist.md`. Then:

```
grafel_findings(action="save", question="What business rules does the <group> group enforce?",
  answer="<file: ~/.grafel/docs/<group>/business/rules/index.md>",
  type="business_rules",)
```

Hand back to the orchestrator.

---

**[pass-18 telemetry]** Print at end of this pass:
```
[pass-18] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
