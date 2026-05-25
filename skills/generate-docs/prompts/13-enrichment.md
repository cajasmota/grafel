# Pass 13 — LLM Enrichment

---

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

## CRITICAL STORAGE DISCIPLINE
===========================
All generated documentation MUST be written under:
  `~/.archigraph/docs/<group>/...`

Determine `<group>` via the `archigraph_whoami` MCP call (the Pre-flight assertion
above). Pass it through every subsequent file write as `${OUTPUT_ROOT}`.

You are STRICTLY FORBIDDEN from writing documentation files into:
- The source repo's working tree (anywhere under `<repo>/docs/`, `<repo>/doc/`, etc.)
- The CWD unless CWD is already inside `~/.archigraph/docs/<group>/`
- Any path that is a git working directory

If you find yourself about to write to a repo path, STOP. The skill assumes
the archigraph-owned store. Writing elsewhere breaks the storage contract
and pollutes the user's source repo.

The daemon dashboard reads from `~/.archigraph/docs/<group>/` -- any output
written elsewhere is invisible to it.

### Pre-flight storage assertion -- SECOND action in this pass

Compute and verify the output root immediately after the `archigraph_whoami` call:

```bash
OUTPUT_ROOT="$HOME/.archigraph/docs/<group>/"   # substitute <group> from whoami
mkdir -p "$OUTPUT_ROOT"
echo "OUTPUT_ROOT=$OUTPUT_ROOT"
```

All file writes in this pass MUST use `${OUTPUT_ROOT}<relative-path>`. Never write to any
other location. If `mkdir -p` fails, ABORT: "Cannot create output directory at $OUTPUT_ROOT."
## CRITICAL OUTPUT DISCIPLINE
==========================
The generate-docs skill produces markdown files in the canonical store
at `~/.archigraph/docs/<group>/`. It does NOT produce:
- VitePress / Docusaurus / Sphinx / mkdocs scaffolding
- `package.json` or any build manifests for static site generators
- Any non-markdown asset that wraps the docs for publishing
- `.gitignore` entries

Publishing is downstream — handled by the archigraph dashboard or
external tooling. If you find yourself about to write a `config.ts`,
`package.json`, `mkdocs.yml`, `.vitepress/config.ts`, or any build
manifest, STOP. The skill's job is content, not infrastructure.




---


Produce structured YAML frontmatter enrichments for every `http_endpoint`,
`process_flow`, and `message_topic` entity in the group. The dashboard Paths,
Flows, and Topology surfaces consume this data to surface summaries, group
badges, rank scores, gap warnings, and disqualification signals.

**Dashboard consumers:**
- `http_endpoint` → Paths panel (`handlers_paths.go`)
- `process_flow` → Flows list + detail panel (`handlers_flows.go`, `enrichmentHealth`)
- `message_topic` → Topology list + detail panel (`handlers_topology.go` `applyTopologyEnrichment`, `handlers_topology_detail.go` `computeEnrichmentHealth`)

> **Pass 3a hook active** for any entity where a doc file is being written
> from scratch (i.e., no existing doc). Before writing the prose section,
> run the generation-time repair hook from `prompts/03a-generation-time-repair.md`.

## Inputs

- Group inventory already produced in Passes 1–2.
- Existing doc files under `~/.archigraph/docs/<group>/<repo-slug>/` (Pass 4–6 output) — enrich in-place.
- `archigraph_enrichments(action=list)` — pre-computed enrichment candidates
  from the daemon; use as signals, not as verbatim output.
- Per-kind templates at `skills/generate-docs/templates/<kind>.md` — use as
  starting point when creating new files.

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

6. **Detect gaps** — use the gap checklist below.

7. **Write per-kind fields** — follow the field guidance for each kind below.

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
- [ ] No `expected_consumers` listed

#### Per-kind field guidance

**`http_endpoint` — Paths panel**

All fields below are consumed by the Paths panel. Populate as many as you have
confident data for:

- `method`, `path` — copy from the entity properties or handler signature.
- `parameters` — list every query/path/header/body parameter with type and
  required flag. Inferred from function signature or docstring.
- `responses` — document at least `200` and the most likely error codes.
  Shape is a compact inline TypeScript-style type string.
- `auth` — one-sentence description of the auth requirement.
- `tables_touched` — list DB tables or ORM models that this handler reads or writes.
- `parameters_explained` — prose expanding on non-obvious parameter semantics.
- `response_shapes_explained` — prose describing nested object structures,
  enum values, and optional fields.
- `examples` — one or two representative call-and-response examples in prose.
- `caveats` — edge cases, rate limits, soft-delete behaviour, deprecation notes.

**`process_flow` — Flows list + detail panel**

The Flows panel's `enrichmentHealth` checks: `summary`, `preconditions`,
`expected_outcome`, `steps`, `gaps`. Populate all five to achieve full health.

- `steps` — numbered list of discrete actions in the flow's happy path.
  Use short imperative phrases (verb + noun). Order must match execution order.
  Aim for 3–10 steps; collapse trivially-small steps (e.g. a single assignment)
  into their parent action.
- `preconditions` — the minimum state required for the flow to begin. One sentence.
- `expected_outcome` — what the system state looks like after a successful run.
  Include persistence, events emitted, and side effects.
- `examples` — prose: happy path narrative + one failure/retry scenario.
- `caveats` — known failure modes, retry behaviour, race conditions, async gaps.

**`message_topic` — Topology list + detail panel**

The Topology detail panel's `computeEnrichmentHealth` checks six fields:
`summary`, `schema`, `volume_estimate`, `typical_payload_size_bytes`,
`expected_consumers`, `gaps`. Populate all six to achieve a `filled_field_count`
of 6/6.

- `purpose` — distinct from `summary`: explain the business reason this topic
  exists and how it fits into the architecture. Consumed by the detail panel prose
  section. Two to four sentences.
- `schema` — inline compact type string representing the message payload.
  Use TypeScript-style notation: `{ field: type, ... }`. If the schema has
  multiple versions, document the latest and note version history in `caveats`.
  **Important:** when `schema` is present in the frontmatter, the Topology detail
  panel prefers it over the entity-derived `schema` property from the graph — so
  this is the authoritative source for the UI.
- `typical_payload_size_bytes` — integer estimate. Useful for capacity planning.
  Omit if you have no data rather than guessing wildly.
- `volume_estimate` — one of `low | medium | high | very-high`. Base on
  known traffic patterns or the flow rank of the publishing flow.
- `expected_consumers` — list of service/repo slugs that subscribe. The Topology
  detail panel merges these hints into the graph-derived related-topics list, so
  include any consumer whose subscription edge may not be in the graph (e.g.
  external services consuming via a shared subscription).
- `examples` — prose: sample publish event with representative field values.
- `caveats` — schema version compatibility, at-least-once vs. exactly-once
  delivery, ordering guarantees, dead-letter-queue status.

### Step 4 — Write frontmatter

For each entity, prepend the YAML frontmatter block to the **top** of the
existing doc file. Do not alter the prose body below the closing `---`.

Use the template for the entity's `kind` as your starting point:

```
skills/generate-docs/templates/http_endpoint.md
skills/generate-docs/templates/process_flow.md
skills/generate-docs/templates/message_topic.md
```

See `skills/generate-docs/examples/` for fully-populated reference examples.

If no doc file exists for the entity, create a minimal file at:

```
~/.archigraph/docs/<group>/<repo-slug>/enrichments/<kind>/<entity_id>.md
```

This path must contain the entity ID as a substring so the backend's
fast-path lookup (`applyTopologyEnrichment` pass 1, `extractFlowDocsWithResolver`
fast path) can resolve it without a full frontmatter scan.

Emit only fields relevant to the entity's `kind`. Do not emit `steps` on an
`http_endpoint` or `method`/`path` on a `message_topic`. Omit any field where
you have no confident data rather than fabricating values.

### Step 5 — Submit enrichment record

After writing the frontmatter, record the enrichment in the daemon so the
group state reflects it and the `docgen_status` transitions to `enriched`:

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
- [ ] `kind` matches the graph entity kind (`http_endpoint`, `process_flow`, or `message_topic`).
- [ ] `rank` is in 0..1 (or absent).
- [ ] `merged_into` references an `entity_id` that exists in this group (or is absent).
- [ ] `disqualified: true` is justified in a `gaps` entry or comment.
- [ ] No per-kind fields from the wrong kind (e.g. no `steps` on an `http_endpoint`, no `method` on a `message_topic`).
- [ ] For `process_flow`: `summary`, `preconditions`, `expected_outcome`, `steps`, and `gaps` are all present (needed for full `enrichment_health`).
- [ ] For `message_topic`: `summary`, `schema`, `volume_estimate`, `typical_payload_size_bytes`, `expected_consumers`, and `gaps` are all present (needed for `filled_field_count` = 6).
- [ ] The doc file path is registered in `docgen-state.json` `GeneratedPaths` so the backend can locate it.

### Step 7 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. Pass 13 reads
`archigraph_expand` neighborhoods for every entity in the working list; this is
rich signal for repair candidates.

Primary discovery types in this pass:

- **`fix_kind`** — the enrichment candidate list from `archigraph_enrichments`
  may contain entities whose graph kind contradicts what you observe. For
  example, a `process_flow` entity that is actually an `http_endpoint` (a
  handler with route metadata), or a `message_topic` catalogued as `Class`.
  Emit a `fix_kind` for each.

  Example:

  ```json
  {
    "type": "fix_kind",
    "source_entity_id": "<entity id>",
    "new_kind": "http_endpoint",
    "confidence": 0.88,
    "evidence": "orders/views.py@line 21: class OrderCreateView(APIView) — has @route decorator; listed as process_flow in graph but is an HTTP endpoint",
    "source": "generate-docs/pass-13",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`merge_flow`** — Step 3b asks you to decide whether two near-duplicate
  entities should be merged (`merged_into`). When you make that decision, also
  emit a `merge_flow` candidate so the graph reflects the merge, not just the
  enrichment YAML.

  Example:

  ```json
  {
    "type": "merge_flow",
    "source_entity_id": "<redundant entity id>",
    "target": "<canonical entity id>",
    "confidence": 0.82,
    "evidence": "payments/handlers.py@line 55 and line 88 — PaymentCreateView and LegacyPaymentCreateView handle identical POST /payments route; canonical is PaymentCreateView",
    "source": "generate-docs/pass-13",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`label_external`** — `FETCHES` edges pointing at external URLs (third-party
  APIs) are often unresolved in the graph. When you document these in
  `http_endpoint` enrichments (the `caveats` or `examples` sections), emit a
  `label_external` for the external target.

Use `source: "generate-docs/pass-13"` in all candidates. Append to
`~/.archigraph/groups/<group>/docgen-repairs.jsonl`.

### Step 8 — Hand back

Save a finding summarising enrichment coverage:

```
archigraph_save_finding(
  question="What enrichment data was produced for this group?",
  answer="Pass 13 enriched <N> http_endpoints, <M> process_flows, <K> message_topics. <X> disqualified. <Y> merged. Health coverage: <Z> message_topics at 6/6, <W> process_flows at 5/5.",
  type="enrichment",
)
```

Hand control back to the orchestrator. The orchestrator marks Pass 13 complete
in docgen-state.json and queues Pass 14 (frontmatter validation).

---

**[pass-13 telemetry]** Print at end of this pass:
```
[pass-13] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
