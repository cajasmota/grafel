# Pass 20 — LLM-Mode Orchestrate (Ticket F)

Read every `*-bundle.json` file in a docgen output directory, call the LLM
section-by-section, assemble an `LLMRunResult`, write a matching
`*-result.json`, and invoke `archigraph docgen --tier=1 --llm-mode=apply` to
produce the final page.

This pass is the **orchestrator side** of the emit-and-orchestrate LLM loop.
It is independent of Passes 0–19 and runs ONLY when a bundle file exists.
The daemon side (emit + apply) is already wired; this pass provides the
Claude Code half.

> **When to run this pass:**
> The user asks to "fill the LLM bundle", "run docgen in LLM mode", or a
> `*-page-bundle.json` file already exists and no matching `*-result.json`
> exists yet.

---

## Inputs

- One or more `*-page-bundle.json` files in a docgen output directory
  (e.g. `~/.archigraph/docs/<group>/.tier1-<ts>/`).
- The `archigraph` binary on PATH (use it verbatim; cross-platform).
- No daemon calls required — the bundle is self-contained.

## Pass procedure

### Step 0 — Locate bundle files

```bash
DOCS_DIR="${DOCS_DIR:-~/.archigraph/docs/<group>}"
find "$DOCS_DIR" -name '*-bundle.json' | sort
```

If `--docs-dir` was supplied by the caller, use that path. If no bundle files
are found, stop and tell the user to run:

```bash
archigraph docgen --tier=1 --group=<group> --seed-entity=<id> --llm-mode=emit
```

### Step 1 — For each bundle file

Repeat Steps 2–5 per bundle file. Process bundles sequentially; do not
attempt parallel LLM calls for separate bundles in the same pass invocation.

Derive the result path from the bundle path:

```
bundle:  <outDir>/<pageID>-page-bundle.json
result:  <outDir>/<pageID>-page-result.json
```

**Skip** a bundle whose matching result file already exists AND whose
`prompt_hash` matches the bundle's `prompt_hash` (idempotent restart support).

### Step 2 — Read and validate the bundle

Read the bundle JSON. Confirm:

- `version == "1"` — if not, print an error and skip this bundle.
- `sections` array is non-empty — if empty, skip with a warning.
- `prompt_hash` is non-empty — required for result assembly.

Record from the bundle:

| Field | Variable |
|-------|----------|
| `version` | `BUNDLE_VERSION` |
| `prompt_hash` | `BUNDLE_HASH` |
| `tier` | `BUNDLE_TIER` |
| `group` | `BUNDLE_GROUP` |
| `seed_entity_id` | `SEED_ENTITY` |
| `graph_context` | available for all section prompts |
| `sections[]` | section prompt list (ordered) |

### Step 3 — Generate prose per section

For **each entry in `bundle.sections`**, run one LLM turn:

**System context (provide once per section call):**
```
You are a technical documentation writer for the archigraph docgen system.
Generate ONLY the markdown body for the section described below.
Do NOT emit frontmatter, headings above the section level, or any wrapper JSON.
Stay within the word budget. Do not exceed the mermaid block budget.
```

**User prompt per section:**
```
Entity: <graph_context.entity_name> (<graph_context.entity_kind>)
Qualified name: <graph_context.qualified_name>
Source file: <graph_context.source_file>
Repo: <graph_context.repo>

Neighbours (1-hop):
<for each neighbour_brief: "- <name> (<kind>) [<relationship>]">

Source window (if non-empty):
<graph_context.source_window>

---
SECTION: <section.section>
GUIDANCE: <section.guidance>
WORD BUDGET: <section.max_words> words maximum
MERMAID BUDGET: <section.max_mermaid> mermaid blocks maximum

Stub (deterministic graph output — use as grounding, replace with real prose):
<section.stub_markdown>
```

**Collect the LLM response** as raw markdown prose. Do not add the section
heading — the `assemblePage` machinery adds it.

**For each collected response, compute:**
- `word_count` = number of whitespace-separated tokens in the response
- `mermaid_count` = number of ` ```mermaid ` fences in the response
- `link_refs` = list of all `[text](./target)` relative links found

### Step 4 — Assemble LLMRunResult

Construct the result object matching the locked schema from `llm_bundle.go`:

```json
{
  "version": "<BUNDLE_VERSION>",
  "prompt_hash": "<BUNDLE_HASH>",
  "tier": <BUNDLE_TIER>,
  "group": "<BUNDLE_GROUP>",
  "seed_entity_id": "<SEED_ENTITY>",
  "section_results": [
    {
      "section": "<section.section>",
      "markdown": "<generated prose>",
      "mermaid_count": <N>,
      "word_count": <N>,
      "link_refs": ["./foo", ...]
    }
  ],
  "filled_at": "<RFC3339 timestamp>"
}
```

**Critical invariants (the daemon's `ApplyResult` will reject the result if
any of these are violated):**

1. `version` must equal the bundle's `version` field exactly.
2. `prompt_hash` must equal the bundle's `prompt_hash` field exactly.
3. `section_results` must contain EXACTLY the same section names as
   `bundle.sections` — no more, no fewer.
4. Every `section` value must be a known section name from KnownSections:
   `overview`, `capabilities`, `flows`, `patterns`, `api`,
   `reference-config`, `reference-dependencies`, `reference-deployment`,
   `reference-scripts`, `reference-misc`, `module-readme`, `glossary`,
   `how-to-local-dev`.

### Step 5 — Write result.json

Write the assembled JSON to the result path:

```
<outDir>/<pageID>-page-result.json
```

Use compact JSON (no trailing whitespace). Confirm the file was written
successfully before proceeding to Step 6.

### Step 6 — Invoke apply

```bash
archigraph docgen \
  --tier=1 \
  --llm-mode=apply \
  --bundle-file="<bundle_path>" \
  --result-file="<result_path>"
```

**Success output** (from the daemon):
```
tier1 apply complete

  entity:     <name>
  sections:   <N>
  contract:   PASS
  output:     <path to final page>
  score:      <path to score.json>
```

**On failure:** the daemon prints a clear error (hash mismatch, missing
section, contract violation). Diagnose and fix before proceeding.

Report to the user:
- Path to the final page markdown.
- Contract status (PASS or list of violations).
- Score fields: `section_count`, `token_count_estimate`,
  `prose_density_words_per_section`.

### Step 7 — Repeat for remaining bundles

Return to Step 1 for each remaining bundle file. After all bundles are
processed, print a summary:

```
LLM orchestrate complete
  bundles processed:  N
  bundles skipped:    N  (already have matching result)
  pages written:      N
  contract failures:  N
```

---

## Error reference

| Error from `archigraph docgen --llm-mode=apply` | Fix |
|-------------------------------------------------|-----|
| `prompt_hash mismatch` | Bundle was re-emitted after result was written. Re-run Step 3–5 against the new bundle. |
| `section coverage error: bundle sections missing from result` | One or more section LLM calls failed silently. Re-generate the missing sections and rebuild the result JSON. |
| `section coverage error: result contains sections not in bundle` | A section was added to the result that is not in the bundle. Remove it. |
| `unmarshal bundle` | Bundle file is corrupt or empty. Re-emit with `--llm-mode=emit`. |

---

## Contract enforcement notes

The `max_words` and `max_mermaid` fields in each `LLMSectionPrompt` are soft
budgets for the LLM response. They are NOT enforced by `ApplyResult` at the
JSON level, but `checkPageContract` runs on the assembled page and violations
appear in `score.json → contract_violations`. Aim to stay within budget to
keep the score clean.

`link_refs` in each section result are used by `ApplyResult` for cross-page
link validation. Populate them accurately.
