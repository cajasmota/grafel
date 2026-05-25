# Pass 16 — Product capabilities (BUSINESS tier)

---

## Staging path

Read `run_id` and `staging_path` from `~/.archigraph/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.archigraph/docs/<group>/`. Wherever this prompt says `~/.archigraph/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use archigraph MCP tools:
`archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`,
`archigraph_clusters`, `archigraph_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `archigraph_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `archigraph_whoami` before doing anything else in this pass. If it errors:
ABORT with: "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

Produce one page per product capability: what the system does and why, in
business language. Capabilities are derived from the system's externally-visible
behaviour — HTTP endpoints, process flows, message topics, scheduled jobs —
grouped by business meaning, NOT one-per-endpoint.

> **READ FIRST:** `snippets/business-voice.md`. Binding. No symbol names, no API
> paths, no code mermaid. PM audience.

Synthesised across the whole group.

## Inputs

- `~/.archigraph/docs/<group>/business/domain-glossary.md` (Pass 15) — link into it.
- Technical-tier `reference/api.md`, module `flows.md`, and `overview.md` for
  every repo (where generated) — these enumerate the behaviour; translate it.
- The graph via the MCP tools below.

## Output

```
~/.archigraph/docs/<group>/business/capabilities/<capability-slug>.md   # one per capability
```

Use `output-templates/business-capability.md`. `<capability-slug>` is a
kebab-case business name (e.g. `schedule-inspection`, `submit-report`,
`manage-deficiencies`) — not a module or endpoint name.

## Procedure

### Step 1 — Enumerate behaviour

```
archigraph_endpoints(repo_filter=null, limit=500)        # what the product exposes
archigraph_flows(repo_filter=null, limit=200)            # process flows / call chains
archigraph_find(question="scheduled jobs and background processing", repo_filter=null, depth=2, token_budget=1000)
archigraph_find(question="message topics and events the system emits", repo_filter=null, depth=2, token_budget=1000)
```

If `archigraph_endpoints` / `archigraph_flows` are unavailable, fall back to
reading the technical-tier `reference/api.md` and module `flows.md`.

### Step 2 — Cluster into capabilities

Group the raw behaviour by **business outcome**. Many endpoints + a flow + a
topic often constitute ONE capability:

> `POST /inspections`, `PATCH /inspections/{id}`, `POST /inspections/{id}/submit`,
> the `inspection.submitted` topic, and the submit flow →
> capability **"Conduct and submit an inspection."**

Aim for a SMALL number of capabilities (typically 5–15 for a product), each
genuinely distinct. Do not emit a capability page per endpoint — that is the
over-fragmentation the audit warned about, in business clothing.

### Step 3 — Write each capability page

Fill `output-templates/business-capability.md`: what it does, why it exists, who
uses it and when, what it produces, the rules that govern it (forward-link to
`rules/` — Pass 18 fills those), related journeys (forward-link to `journeys/` —
Pass 17). Plain language throughout.

### Step 4 — Anchors + provenance

Headings first, then derive `anchors:` per `snippets/anchor-contract.md`. Code
references only in the collapsed `<details>` block.

### Step 5 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. This pass
calls `archigraph_endpoints` and `archigraph_flows`, which surfaces concrete
entity data. The expected repair types are:

- **`merge_flow`** — Step 2's clustering often reveals two process-flow or
  flow entities that represent the same capability. When you collapse multiple
  flows into one capability, emit a `merge_flow` candidate for the redundant
  flow entity pointing at the canonical one.

  Example — two checkout flows collapsed into one "Conduct checkout" capability:

  ```json
  {
    "type": "merge_flow",
    "source_entity_id": "<checkout_legacy_flow entity id>",
    "target": "<checkout_flow entity id>",
    "confidence": 0.78,
    "evidence": "archigraph_flows result: checkout_legacy_flow and checkout_flow both terminate at OrderConfirmed — same business outcome, merged into capability 'conduct-checkout'",
    "source": "generate-docs/pass-16",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`fix_kind`** — `archigraph_endpoints` may return entities that are
  catalogued as `Function` but are clearly HTTP endpoints (they have a route
  path and method). Emit a `fix_kind` if you observe this.

Only emit when `archigraph_endpoints` / `archigraph_flows` data or a direct
`archigraph_expand` result provides concrete evidence. Business-layer
clustering reasoning alone does not reach the 0.5 threshold.

Use `source: "generate-docs/pass-16"` in all candidates. Append to
`~/.archigraph/groups/<group>/docgen-repairs.jsonl`.

### Step 6 — Verify + save

Run `snippets/verification-checklist.md`. Then once, for the capability set:

```
archigraph_save_finding(
  question="What product capabilities does the <group> group provide?",
  answer="<files: ~/.archigraph/docs/<group>/business/capabilities/*.md>",
  type="business_capabilities",
)
```

Hand back. Pass 19 (business overview) will index every capability page you
wrote, so report the list of capability slugs to the orchestrator.

---

**[pass-16 telemetry]** Print at end of this pass:
```
[pass-16] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
