# Pass 17 — User journeys as business narrative (BUSINESS tier)

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

Produce end-to-end user journeys written as PLAIN-LANGUAGE narratives — a user
accomplishing a goal across the whole product. This pass exists specifically to
fix the audit finding that the old `user-journeys.md` was a 60-step mermaid
sequence diagram naming internal symbols. That artifact belongs in the technical
tier; here we write the business version.

> **READ FIRST:** `snippets/business-voice.md`. Binding. The hard rule: NO code
> sequence diagrams, NO internal symbols, NO API paths. A simple business-step
> `flowchart` (≤ 8 business-labelled boxes) is the ONLY diagram allowed, and only
> if it adds something the prose doesn't.

Synthesised across the whole group — a journey typically crosses repos (mobile
app → backend → office web).

## Inputs

- `~/.grafel/docs/<group>/business/domain-glossary.md` (Pass 15).
- `~/.grafel/docs/<group>/business/capabilities/*.md` (Pass 16) — link into them.
- Process flows / call chains and cross-repo links from the graph.
- Any technical-tier journey/flow pages — translate to business voice, do not
  copy. Demote their mermaid sequence diagrams; they stay in the technical tier.

## Output

```
~/.grafel/docs/<group>/business/journeys/<journey-slug>.md   # one per journey
```

Use `output-templates/business-journey.md`. A journey-slug is a goal in
kebab-case (e.g. `field-inspection-day`, `customer-places-order`,
`onboard-new-building`).

## Procedure

### Step 1 — Identify the goals

A journey is a goal a real user accomplishes, end to end. Find them from:

```
grafel_flows(repo_filter=null, limit=200)
grafel_traces(action=list, repo_filter=null, limit=50)
grafel_cross_links(action=list, limit=200)   # cross-repo legs of a journey
```

Plus the capability set from Pass 16 (a journey usually chains several
capabilities). Pick the handful of journeys that matter to the business
(typically 3–8). Do not enumerate every code path.

### Step 2 — Write the narrative

A NUMBERED list of plain-language steps: what the user does, sees, decides; what
the system does for them — in business terms. Cross-repo legs become natural
sentences ("the inspection syncs to the office when the device is back online"),
never "`core-mobile` calls `upvate_core` `/api/v1/sync`".

Then "What can go wrong" (business exceptions: offline, rejected, missing data)
and "Where it touches the business" (link to the capabilities and domain terms
it exercises).

### Step 3 — Optional business diagram

If a ≤8-box business `flowchart` clarifies the sequence, add it with
business-only labels. Otherwise omit. NEVER a `sequenceDiagram`.

### Step 4 — Anchors + provenance

Headings first, derive `anchors:` per `snippets/anchor-contract.md`. Symbols and
file paths ONLY in the collapsed `<details>` block.

### Step 5 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. This pass
calls `grafel_flows`, `grafel_traces`, and `grafel_cross_links`.
The expected repair types here are narrow:

- **`merge_flow`** — Step 1 may reveal two flow entities returned by
  `grafel_flows` that represent legs of the same end-to-end journey (e.g.
  `sync_to_office_flow` and `offline_sync_flow` are the same flow triggered
  from two contexts). Emit a `merge_flow` only when the evidence from
  `grafel_traces` or `grafel_cross_links` makes the identity
  unambiguous (confidence ≥ 0.75).

  Example:

  ```json
  {
    "type": "merge_flow",
    "source_entity_id": "<offline_sync_flow entity id>",
    "target": "<sync_to_office_flow entity id>",
    "confidence": 0.76,
    "evidence": "grafel_traces result: offline_sync_flow and sync_to_office_flow share identical call chain terminating at SyncService.commit — same business journey, different connectivity contexts",
    "source": "generate-docs/pass-17",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

Do not emit candidates derived purely from business-narrative reasoning.
Cross-link and trace data must back any emission.

Use `source: "generate-docs/pass-17"` in all candidates. Append to
`~/.grafel/groups/<group>/docgen-repairs.jsonl`.

### Step 6 — Verify + save

Run `snippets/verification-checklist.md`. Then:

```
grafel_save_finding(
  question="What are the business user journeys for the <group> group?",
  answer="<files: ~/.grafel/docs/<group>/business/journeys/*.md>",
  type="business_journeys",
)
```

Hand back; report the list of journey slugs for Pass 19's index.

---

**[pass-17 telemetry]** Print at end of this pass:
```
[pass-17] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
