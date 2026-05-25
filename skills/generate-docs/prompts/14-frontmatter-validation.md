# Pass 14 — Frontmatter Validation

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


Validate every enriched doc file written by Pass 13. Catch schema drift between
the skill output and the backend parser's expectations before the user sees a
blank panel in the dashboard.

> **Read-only pass** — Pass 14 does not modify any doc file. It only reports.
> Re-run Pass 13 to fix any entity with validation failures.

## Inputs

- `docgen-state.json` `GeneratedPaths` list — the set of files to validate.
- The enriched doc files themselves.
- The field matrix in `SKILL.md § Per-kind field matrix` — source of truth for
  which fields are consumed vs. prose-only.

## Procedure

### Step 1 — Load file list

Read `docgen-state.json` for the group and collect all entries in `GeneratedPaths`.
Filter to files that contain a frontmatter block (i.e., the file begins with `---`).

### Step 2 — Per-file validation

For each file in the filtered list, run all checks below. Record pass/fail per
check per file.

#### Check A — Structural validity

- [ ] File begins with `---` on line 1 (no leading blank line or BOM).
- [ ] There is a second `---` line that closes the frontmatter block.
- [ ] The YAML between the delimiters parses without syntax errors (no unquoted
  colons in values, no mismatched list indentation).

#### Check B — `kind` field

- [ ] `kind` is present.
- [ ] `kind` is exactly one of: `http_endpoint`, `process_flow`, `message_topic`.
- [ ] `kind` matches the entity kind recorded in the daemon for this `entity_id`
  (if `entity_id` is present and resolvable via `archigraph_inspect`).

#### Check C — Kind isolation

No cross-kind fields present. Specifically:

- `http_endpoint`: must NOT contain `steps`, `preconditions`, `expected_outcome`,
  `schema`, `typical_payload_size_bytes`, `volume_estimate`, `expected_consumers`,
  `purpose`.
- `process_flow`: must NOT contain `method`, `path`, `parameters`, `responses`,
  `auth`, `tables_touched`, `schema`, `typical_payload_size_bytes`,
  `volume_estimate`, `expected_consumers`, `purpose`.
- `message_topic`: must NOT contain `method`, `path`, `parameters`, `responses`,
  `auth`, `tables_touched`, `steps`, `preconditions`, `expected_outcome`.

#### Check D — Scalar field types

- [ ] `rank` (if present) is a float in `[0.0, 1.0]`.
- [ ] `typical_payload_size_bytes` (if present) is a positive integer.
- [ ] `disqualified` (if present) is `true` or `false` (not a string).
- [ ] `volume_estimate` (if present) is one of `low`, `medium`, `high`, `very-high`.

#### Check E — Referential integrity

- [ ] `merged_into` (if non-empty) references an `entity_id` that appears in at
  least one other doc file in this group or is resolvable via `archigraph_inspect`.

#### Check F — Health-tracked field coverage

Report missing fields per kind (these are the fields that determine `enrichment_health`):

**`message_topic`** (backend `computeEnrichmentHealth`, total = 6):
- `summary` — present?
- `schema` — present?
- `volume_estimate` — present?
- `typical_payload_size_bytes` — present and > 0?
- `expected_consumers` — present and non-empty?
- `gaps` — present and non-empty?

**`process_flow`** (backend `enrichmentHealth`):
- `summary` — present?
- `preconditions` — present?
- `expected_outcome` — present?
- `steps` — present and non-empty?
- `gaps` — present and non-empty?

**`http_endpoint`** (no structured health check in current backend, but completeness still matters):
- `summary` — present?
- `method` — present?
- `path` — present?
- `auth` — present?

#### Check G — Discovery-path reachability

The backend locates enrichment docs via path matching, not just a DB lookup.
Verify the matching strategy will succeed for each file:

For `message_topic` files: the path must contain either the `entity_id` substring
OR contain `"topic"` or `"topology"` (case-insensitive) so `applyTopologyEnrichment`
pass 2 can reach it.

For `process_flow` files: the path must contain either the `entity_id` substring
OR contain `"flow"` (case-insensitive) so `extractFlowDocsWithResolver` fast-path
can reach it. If neither matches, the tertiary pass will still work if `entity_id`
is set in the frontmatter — but flag it as a discovery risk.

For `http_endpoint` files: the path must contain the `entity_id` substring, or
`entity_id` must be set in the frontmatter for fallback matching.

### Step 3 — Compile results

Produce a structured summary with:

- Total files scanned.
- Pass count (all checks A–G passed).
- Fail count, broken down by check.
- For each failing file: the file path, which checks failed, and the specific
  problem (e.g. "Check C: `steps` found on `http_endpoint`").
- For each entity with incomplete health coverage: the entity ID, kind, and the
  list of missing health-tracked fields.

### Step 4 — Save finding

```
archigraph_save_finding(
  question="Pass 14 frontmatter validation report",
  answer="<Structured summary: N files scanned, M passed, K failed. Failing files: [list]. Incomplete health: [list]>",
  type="enrichment_validation",
)
```

### Step 5 — Hand back

Report to the orchestrator:

- If **all checks passed**: mark Pass 14 complete. Passes 13–14 are done.
- If **any checks failed**: list the affected entity IDs. The orchestrator should
  queue a targeted re-run of Pass 13 for those entities only, then re-run Pass 14
  to confirm. Do not mark Pass 14 complete until no failures remain.

Pass 14 does **not** modify doc files. It only reports. The fix is always a
Pass 13 re-run for the affected entity.

---

**[pass-14 telemetry]** Print at end of this pass:
```
[pass-14] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
