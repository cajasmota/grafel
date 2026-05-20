# Pass 13 — LLM Enrichment

Produce structured YAML frontmatter enrichments for every `http_endpoint`,
`process_flow`, and `message_topic` entity in the group. The dashboard Paths,
Flows, and Topology surfaces consume this data to surface summaries, group
badges, rank scores, gap warnings, and disqualification signals.

> **Pass 3a hook active** for any entity where a doc file is being written
> from scratch (i.e., no existing doc). Before writing the prose section,
> run the generation-time repair hook from `prompts/03a-generation-time-repair.md`.

## Inputs

- Group inventory already produced in Passes 1–2.
- Existing doc files under `<repo>/docs/` (Pass 4–6 output) — enrich in-place.
- `archigraph_enrichments(action=list)` — pre-computed enrichment candidates
  from the daemon; use as signals, not as verbatim output.

## Procedure

### Step 1 — Collect entities

```
archigraph_find(question="HTTP endpoints routes handlers", depth=1, token_budget=1500)
archigraph_find(question="process flows call chains entry points", depth=1, token_budget=1500)
archigraph_find(question="message topics broker queues publishers consumers", depth=1, token_budget=900)
```

Build a working list of entity IDs grouped by kind.

### Step 2 — Enrichment candidates queue

```
archigraph_enrichments(action=list, kind="http_endpoint")
archigraph_enrichments(action=list, kind="process_flow")
archigraph_enrichments(action=list, kind="message_topic")
```

Merge candidates into your working list; they carry pre-computed signals
(caller counts, inbound FETCHES, PUBLISHES_TO edge counts) that inform `rank`.

### Step 3 — Per-entity enrichment

For each entity in the working list:

1. **Inspect neighbours** — understand auth edges, QUERIES edges, PUBLISHES_TO,
   inbound FETCHES:

   ```
   archigraph_expand(node="<entity_id>", depth=2)
   ```

2. **Decide merge / disqualify** — if two entities are near-duplicates (same
   logical endpoint in two controllers, or same topic under two names),
   pick the canonical one and set `merged_into` on the redundant one.
   If an entity is clearly a false positive (test fixture, regex stub), set
   `disqualified: true`. Do not disqualify real signal; when in doubt, leave
   `disqualified: false`.

3. **Compute rank** — use inbound caller count + business criticality
   heuristic (payments, auth, data-integrity operations rank higher). Range
   is 0..1; omit if you have no signal.

4. **Assign group** — infer a domain cluster key from the URL prefix / entity
   name / handler file path. Use short lower-case keys: `auth`, `orders`,
   `inventory`, `users`, `billing`, `notifications`, etc.

5. **Write summary** — one sentence, no jargon, from the user's perspective.

6. **Detect gaps** — use the checklist below.

#### Gap checklist

For `http_endpoint`:
- [ ] No 4xx error response documented
- [ ] Auth requirement not enforced (no auth edge, endpoint name suggests sensitive data)
- [ ] Mismatched response shape (handler returns more/fewer keys than documented)
- [ ] No parameter validation evident

For `process_flow`:
- [ ] Flow ends without persisting a result (no QUERIES/WRITES_TO edges at terminal)
- [ ] Missing error path (no error-handling branch visible in the call chain)
- [ ] Precondition not enforced (e.g. user auth check absent)

For `message_topic`:
- [ ] Orphan producer (no SUBSCRIBES_TO consumer anywhere in the group)
- [ ] Orphan consumer (no PUBLISHES_TO producer anywhere in the group)
- [ ] Incompatible schemas (two consumers expect different field sets)
- [ ] No expected_consumers listed

### Step 4 — Write frontmatter

For each entity, prepend the YAML frontmatter block to the **top** of the
existing doc file. Do not alter the prose body below the closing `---`.

If no doc file exists for the entity, create a minimal file:

```
<repo>/docs/enrichments/<kind>/<entity_id>.md
```

containing only the frontmatter block followed by a one-paragraph prose
description.

Use the schema documented in `SKILL.md § Unified frontmatter schema`. Emit
only fields relevant to the entity's `kind`. Omit fields where you have no
confident data rather than fabricating values.

### Step 5 — Submit enrichment record

After writing the frontmatter, record the enrichment in the daemon so the
group state reflects it:

```
archigraph_enrichments(
  action=submit,
  entity_id="<id>",
  summary="<one-sentence summary>",
  kind="<kind>",
)
```

### Step 6 — Verification

For each written file, run `snippets/verification-checklist.md`. In addition,
verify:

- [ ] Frontmatter opens with `---` on line 1.
- [ ] Frontmatter closes with `---` before any prose.
- [ ] `kind` matches the graph entity kind.
- [ ] `rank` is in 0..1 (or absent).
- [ ] `merged_into` references an entity_id that exists in this group (or is absent).
- [ ] `disqualified: true` is justified in a `gaps` entry or comment.
- [ ] No per-kind fields from the wrong kind (e.g. no `steps` on an `http_endpoint`).

### Step 7 — Hand back

Save a finding summarising enrichment coverage:

```
archigraph_save_finding(
  question="What enrichment data was produced for this group?",
  answer="Pass 13 enriched <N> http_endpoints, <M> process_flows, <K> message_topics. <X> disqualified. <Y> merged.",
  type="enrichment",
)
```

Hand control back to the orchestrator. The orchestrator marks Pass 13 complete
in docgen-state.json.
