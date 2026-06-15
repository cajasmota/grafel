# Pass 15 — Business domain model + glossary (BUSINESS tier)

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_expand`, `grafel_stats`,
`grafel_clusters`, `grafel_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_whoami` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

The first business pass. Produce the product's business vocabulary: the nouns a
PM uses (Inspection, Deficiency, Jurisdiction, Customer, Order), each defined in
plain language. Later business passes (capabilities, journeys, rules) link into
this glossary, so it runs first.

> **READ FIRST:** `snippets/business-voice.md`. Every rule there is binding for
> this pass. Zero internal symbol names; PM audience; provenance only in the
> collapsed `<details>` block.

The business tier is **synthesised across the whole group**, not per repo. Even
though the file is written under one repo's `docs/business/` (see Output), its
content spans every repo's domain.

## Inputs

- `~/.grafel/groups/<group>/domain.md` — owner-supplied domain framing (Pass 0).
- Every `~/.grafel/docs/<group>/<repo-slug>/overview.md` and `~/.grafel/docs/<group>/<repo-slug>/glossary.md` from the
  technical tier, if it was generated (the technical glossary is symbol-anchored;
  translate it to business voice — do not copy it).
- The graph (entities / data model) via the MCP tools below.

## Output

Write a single group-synthesised file:

```
~/.grafel/docs/<group>/business/domain-glossary.md
```

`<primary-repo>` is the group's anchor repo — the one the orchestrator selected
in the tier-selection step (default: the backend/service repo with the most
entities; the webui aggregates all repos' `business/` trees into one Business
view, so a single location reads as one set). Use
`output-templates/business-domain-glossary.md`.

## Procedure

### Step 1 — Gather candidate domain concepts

Find the business entities (data model), not the code structure:

```
grafel_find(question="core data model and domain entities", repo_filter=null, depth=2, token_budget=1500)
grafel_find(question="what are the main business records the system stores", repo_filter=null, depth=2, token_budget=1200)
```

Also read each technical-tier `glossary.md` and `overview.md`. These already
name the domain; your job is to lift the **business** terms and drop the code.

### Step 2 — Group by business meaning

Multiple classes/tables often back ONE business concept (e.g. `Inspection`,
`InspectionItem`, `InspectionSerializer`, `inspection` table → the single
business term **Inspection**). Collapse them. The reader sees one term, not
four code artifacts.

Discard pure-plumbing entities (serializers, view-sets, repositories, DTOs) —
they are not domain concepts.

### Step 3 — Define each term in business language

For each business term: a plain-language definition, what it relates to (linked
to other glossary terms), and its lifecycle in business words if it has states.
Order alphabetically. Follow `output-templates/business-domain-glossary.md`.

### Step 4 — Anchors + provenance

Write headings first, then derive the `anchors:` frontmatter list per
`snippets/anchor-contract.md`. Put any code/file references ONLY inside the
collapsed `<details>` provenance block at the bottom.

### Step 5 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`, but only for
observations made while reading the graph — not from business-voice prose.

This pass calls `grafel_find` against the data model and reads entity
neighborhoods. The expected repair type here is narrow but high-value:

- **`fix_kind`** — while collapsing code entities into business terms (Step 2),
  you may notice an entity that the graph catalogued as the wrong kind. For
  example, a serializer class that is actually a domain model for business
  purposes (though this is usually a legitimate plumbing entity to discard — use
  your judgment; only emit if the kind in the graph is factually wrong).

  Example:

  ```json
  {
    "type": "fix_kind",
    "source_entity_id": "<entity id>",
    "new_kind": "Class",
    "confidence": 0.85,
    "evidence": "inspections/models.py@line 8: class Inspection(models.Model) — catalogued as Function in graph; is a Django ORM model class",
    "source": "generate-docs/pass-15",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

Only emit when the evidence is concrete (you read the source or a graph property
confirms it). Business-tier reasoning alone does not reach confidence 0.5; skip
emission for glossary judgments that are purely interpretive.

Use `source: "generate-docs/pass-15"` in all candidates. Append to
`~/.grafel/groups/<group>/docgen-repairs.jsonl`.

### Step 6 — Verify + save

Run `snippets/verification-checklist.md` (business-voice section applies).

```
grafel_save_finding(
  question="What is the business domain glossary for the <group> group?",
  answer="<file: ~/.grafel/docs/<group>/business/domain-glossary.md>",
  type="business_domain",
)
```

Hand back to the orchestrator. Pass 16 (capabilities) and Pass 17 (journeys)
both link into this glossary, so they run after it.

---

**[pass-15 telemetry]** Print at end of this pass:
```
[pass-15] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
