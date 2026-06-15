---
name: grafel-test-page
description: Test the grafel docgen LLM-mode loop end-to-end on a single entity. Used for iteration, smoke-testing, and verifying the emit→fill→apply roundtrip without manual command-construction.
when-to-use: User asks to "test docgen on <entity>", "smoke-test docgen", "iterate on docgen prose for X", or invokes /test-docgen-page or /grafel-test-page.
---

# grafel-test-page

Single-entity docgen smoke test with full emit→fill→apply roundtrip in one invocation.

This skill drives the complete `grafel docgen` LLM-mode loop — emit, fill (YOU are the LLM), and apply — on a single entity, in one natural-language invocation. No manual command-construction or copy-paste of paths or schema instructions required.

---

## When to use this skill

Invoke when the user asks for any of:

- "Test docgen on `<entity>`."
- "Smoke-test docgen for `<entity>` in group `<group>`."
- "Iterate on docgen prose for X."
- "Run /test-docgen-page" or "/grafel-test-page" (slash commands).
- "Check the emit→fill→apply roundtrip for `<entity>`."

**Scope:** this skill operates on **ONE entity per invocation**. It is designed for **debugging and iteration** — verifying the roundtrip, tuning guidance prose, and catching contract failures on a specific node.

For production multi-entity docgen across a full group, use the `generate-docs` skill instead. That skill runs Passes 0–20 across every entity in the group; this skill runs a single entity to tight iteration speed.

---

## Inputs

Parse from the invocation text. Only prompt the user when a required field is genuinely ambiguous.

| Input | Required | Default | Notes |
|---|---|---|---|
| `group` | yes | — | e.g. `upvate`, `grafel`, `polyglot-platform` |
| `entity` | yes | — | Prefixed ID (`upvate-core::<hex>`), raw 16-char hex, or free-text query |
| `tier` | no | `1` | `0` = single-section, `1` = single-page |
| `section` | only when tier=0 | — | Section name from KnownSections (e.g. `overview`, `flows`) |
| `output-dir` | no | `/tmp/docgen-test-<RFC3339-timestamp>/` | Where emit, bundle, result, and final page land |
| `show-stub` | no | `false` | If `true`, print first 30 lines of stub-page.md after emit |

Construct the RFC3339 timestamp for the default output-dir at invocation time (e.g. `2026-05-23T14:05:00Z`). Never reuse a previous output-dir unless the user explicitly provides one.

---

## Steps

### Step 1 — Resolve entity

**If** the entity string matches `^([a-z0-9_-]+::)?[0-9a-f]{16}$`, use it directly — skip MCP lookup.

**Otherwise**, resolve via mcp-bridge:

```
grafel_find query=<input> group=<group>
```

The post-#1849 multi-repo format groups results by repo with top-3 hits each. Pick the row with the **highest SCORE across the entire response**, not the first row. A lower-ranked row in the first repo may score lower than the top hit in a second repo.

If `repo_filter` is needed to narrow results (ambiguous common name, large group), add it.

Report the resolution to the user before proceeding:

```
Resolved "<input>" → <name> (<id>) at <source_file>:<line>
```

If no hit is found, stop and ask the user to confirm the entity name or provide an ID directly.

---

### Step 2 — Emit

Run:

```bash
grafel docgen --tier=<tier> --group=<group> --seed-entity=<id> \
  --llm-mode=emit \
  --output-dir=<output-dir>
```

For tier=0, append `--section=<section>`.

**On failure:** report the exact error output and stop. Do not attempt to patch around it.

**On success:**

- Parse `<output-dir>/score.json`. Capture `section_count`, `token_count_estimate`, and stub statistics.
- Identify the bundle file at `<output-dir>/<page-id>-page-bundle.json`. The `page-id` is the entity's short name slug or the `PageID` field from the bundle JSON.
- If `show-stub=true`, print the first 30 lines of `<output-dir>/<page-id>-stub-page.md` (or the closest `-stub*.md` file in the output-dir).

---

### Step 3 — Fill (YOU are the LLM)

Read the bundle at `<output-dir>/<page-id>-page-bundle.json`.

For **each section** in `bundle.sections[]`:

- Use `guidance` as the section instruction. Follow it precisely.
- Use `graph_context.source_window` as ground truth for code references. Cite specific code patterns from it when relevant.
- Use `graph_context.neighbour_briefs` for relationship context — callers, callees, implementors, etc.
- Honor `max_words` and `max_mermaid` budgets. Count honestly.
- **DON'T FABRICATE.** If context is thin, write a short honest "limited context" note rather than padding prose with plausible-sounding details.
- **DON'T BE SYCOPHANTIC.** No "this excellent method…" or "elegantly designed…" framing. Plain, accurate technical prose.
- Match the entity's actual complexity — a simple two-line helper gets a short paragraph; a complex orchestrator warrants more detail.

Assemble an `LLMRunResult` JSON exactly matching `internal/docgen/llm_bundle.go::LLMRunResult`:

```json
{
  "version": "1",
  "prompt_hash": "<copy bundle.prompt_hash byte-for-byte; apply rejects on mismatch>",
  "tier": <bundle.tier>,
  "group": "<bundle.group>",
  "seed_entity_id": "<bundle.seed_entity_id>",
  "section_results": [
    {
      "section": "<section name>",
      "markdown": "<LLM-generated prose>",
      "mermaid_count": 0,
      "word_count": 0,
      "link_refs": []
    }
  ],
  "filled_at": "<RFC3339 UTC timestamp>"
}
```

Rules for `section_results`:

- MUST cover **exactly** the sections in `bundle.sections[]`, same order.
- No extra sections. No missing sections. The daemon rejects mismatches.
- `mermaid_count` = number of ` ```mermaid ``` ` blocks in `markdown`.
- `word_count` = word count of `markdown` (split on whitespace).
- `link_refs` = list of relative markdown links found in `markdown` (e.g. `["../foo/bar.md"]`). Empty list if none.

Write the assembled JSON to `<output-dir>/<page-id>-page-result.json`.

---

### Step 4 — Apply

Run:

```bash
grafel docgen --tier=<tier> --group=<group> --seed-entity=<id> \
  --llm-mode=apply \
  --bundle-file=<output-dir>/<page-id>-page-bundle.json \
  --result-file=<output-dir>/<page-id>-page-result.json \
  --output-dir=<output-dir>
```

**On rejection**, report the **specific** error returned by apply:

- `prompt_hash mismatch` — the `prompt_hash` in your result.json doesn't match the bundle. Recheck Step 3: copy byte-for-byte from `bundle.prompt_hash`.
- `section coverage` — missing or extra sections in `section_results`. Recheck the section list order and names against `bundle.sections[].section`.
- `contract violation` — a section exceeded `max_words` or `max_mermaid`. Identify which section and trim.

Do **not** retry blindly. Diagnose the specific rejection reason, fix result.json precisely, then re-run apply once.

---

### Step 5 — Report

Present the following to the user:

**Score summary** (key fields from `<output-dir>/score.json`):

| Field | Value |
|---|---|
| `llm_mode` | — |
| contract status | — |
| `section_count` | — |
| `token_count_estimate` | — |
| `internal_link_count` | — |
| `internal_link_unresolved` | — |
| `mermaid_count` | — |
| `mermaid_oversized` | — |
| `duplicated_flow_count` | — |

**First ~50 lines of final page.md** (from `<output-dir>/<page-id>-page.md`).

**Brief quality assessment:**

- Which sections came out strong (good source grounding, clear structure, within budget).
- Which sections came out weak (thin context, fabricated-looking prose, mermaid absent when useful).
- Suggested guidance prompt tweaks to iterate. Reference `defaultSectionGuidance` in `internal/docgen/llm_bundle.go` for the current defaults — note which ones to tighten or expand.

**Full page path note:**

```
Full page at: <output-dir>/<page-id>-page.md
```

---

## Pitfalls to avoid

- **Don't paste-and-pray.** If the bundle shape surprises you (unexpected sections, empty source_window, missing fields), surface the anomaly before writing result.json.
- **Don't pad sections.** Honest brevity beats fabricated detail. An empty source_window is a data signal, not a prompt to invent.
- **Don't run apply twice on the same result.json without editing it.** If apply rejects, fix the specific field in result.json; don't re-run apply unchanged.
- **prompt_hash MUST match byte-for-byte.** Do not recompute it. Copy it verbatim from `bundle.prompt_hash`. Any difference — trailing whitespace, case, encoding — causes apply to reject with `prompt_hash mismatch`.
- **Don't mix up page-id and entity-id.** The bundle filename uses `page_id` (the slug); `seed_entity_id` is the hex ID used in CLI flags.

---

## Schema references

- **`internal/docgen/llm_bundle.go`** — canonical struct definitions for `LLMPromptBundle`, `LLMSectionPrompt`, `LLMGraphContext`, `LLMSectionResult`, `LLMRunResult`, and `defaultSectionGuidance`.
- **`skills/generate-docs/prompts/20-llm-orchestrate.md`** — the full Pass 20 procedure for the multi-entity LLM orchestration loop. If you find yourself needing to process more than one entity, stop and switch to `generate-docs` instead.

---

## Constraints

- **ONE entity per invocation.** For multi-entity runs, use the `generate-docs` skill.
- **Never write into source repos.** All output goes to `--output-dir` or `/tmp/`. Do not write any file under the group's source tree.
- **Never restart the daemon or modify the graph.** This skill is read-and-emit only. If the daemon is not running, stop and report it rather than attempting to start it.
