# Pass 18 — Business rules / requirements (BUSINESS tier)

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

- `~/.archigraph/docs/<group>/business/domain-glossary.md` (Pass 15).
- `~/.archigraph/docs/<group>/business/capabilities/*.md` (Pass 16) — rules attach to
  capabilities.
- The graph: validation logic, conditional branches, permission checks,
  required fields.

## Output

```
~/.archigraph/docs/<group>/business/rules/index.md   # grouped by business area
```

Use `output-templates/business-rules.md`. One file is usually enough; split into
`rules/<area>.md` only if a single area has many rules and the index grows
unwieldy. If you split, `rules/index.md` must still exist and link to each area
file (link-hygiene: no bare-directory links).

## Procedure

### Step 1 — Mine the implementation for rules

```
archigraph_find(question="validation rules and required fields", repo_filter=null, depth=2, token_budget=1500)
archigraph_find(question="permission and authorization checks — who can do what", repo_filter=null, depth=2, token_budget=1500)
archigraph_find(question="state transitions and status constraints", repo_filter=null, depth=2, token_budget=1200)
archigraph_find(question="business invariants and conditional logic that rejects input", repo_filter=null, depth=2, token_budget=1200)
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
`archigraph_find` queries on validation, permission checks, and state
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

Only emit when `archigraph_find` returned an entity that you then inspected
closely enough to have concrete file-level evidence. Business rule translation
itself is not evidence.

Use `source: "generate-docs/pass-18"` in all candidates. Append to
`~/.archigraph/groups/<group>/docgen-repairs.jsonl`.

### Step 5 — Verify + save

Run `snippets/verification-checklist.md`. Then:

```
archigraph_save_finding(
  question="What business rules does the <group> group enforce?",
  answer="<file: ~/.archigraph/docs/<group>/business/rules/index.md>",
  type="business_rules",
)
```

Hand back to the orchestrator.
