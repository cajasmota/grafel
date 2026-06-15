# Pass 3 — Repo Overview

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

Write `~/.grafel/docs/<group>/<repo-slug>/overview.md` for every repo in the group. The overview is the entry point a new engineer reads first; it is also the page Pass 7 quotes when synthesizing the group-level page.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

## Inputs

- `~/.grafel/groups/<group>/domain.md`
- `~/.grafel/groups/<group>/inventory.json`
- `~/.grafel/groups/<group>/plan.json`
- The convention file named in the plan for this repo (under `conventions/`).
- `output-templates/overview.md` — fill this template, do not invent a new structure.

## Procedure

For each repo `<r>`:

### Step 1 — Confirm scope

Call `grafel_whoami` with `cwd=<repo absolute path>` so subsequent calls scope correctly. Then `grafel_stats(repo_filter=["<r>"])` to confirm the inventory numbers match.

### Step 2 — Identify the architectural skeleton

Call:

```
grafel_find(question="entry points", repo_filter=["<r>"], depth=2, token_budget=600)
grafel_find(question="public API surface", repo_filter=["<r>"], depth=2, token_budget=600)
grafel_find(question="data model", repo_filter=["<r>"], depth=2, token_budget=600)
```

Use the convention file's `entry_points` section to know what "entry point" means for this stack. For example, in `django.md` it means URLConf root + `wsgi.py` + management commands; in `react.md` it means the router root + the top-level `App` component.

### Step 3 — Identify cross-repo edges

```
grafel_cross_links(action=list, repo_filter=["<r>"], limit=20)
```

Mention any accepted cross-repo links in a section called "Connections to other repos". Pending candidates go in a "Pending links" callout; do not assert them as facts.

### Step 4 — Render

Open `output-templates/overview.md`, fill every section, write the result to `~/.grafel/docs/<group>/<repo-slug>/overview.md`. Apply `_graph-searchability.md` and the stack convention strictly:

- Every code identifier in headings goes in backticks: `` ## `OrderViewSet` ``.
- Every code block has a language tag.
- Module names listed in the overview should match the slugs in `plan.json` exactly so Pass 4's deep-dives are reachable from the overview by relative link.

### Step 5 — Verification

Run `snippets/verification-checklist.md`. If any check fails, fix and re-run before moving on.

### Step 6 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. As you wrote
the overview you read source files and reasoned about entities — collect any
observations that qualify as repair candidates (resolved stubs, dynamic
dispatch targets, mis-classified kinds, external library references). Append
them to `~/.grafel/groups/<group>/docgen-repairs.jsonl`, one JSON object
per line, and record the emission summary line in your pass report.

Common discoveries in this pass:
- Entry-point files that import modules whose stubs are unresolved in the graph.
- Cross-repo links whose direction or target is ambiguous in the graph but clear
  from the source you just read.

Use `source: "generate-docs/pass-3"` in every candidate emitted here.

### Step 7 — Save the result

Call:

```
grafel_save_finding(
  question="What is the architectural overview of <repo>?",
  answer="<file: ~/.grafel/docs/<group>/<repo-slug>/overview.md>",
  type="overview",
  repo_filter=["<r>"]
)
```

This creates a memory entry the future grooming agents can find by query.

When all repos are done, hand control back to the orchestrator.

---

**[pass-03 telemetry]** Print at end of this pass:
```
[pass-03] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
