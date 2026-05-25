# Pass 3 — Repo Overview

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


Write `~/.archigraph/docs/<group>/<repo-slug>/overview.md` for every repo in the group. The overview is the entry point a new engineer reads first; it is also the page Pass 7 quotes when synthesizing the group-level page.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

## Inputs

- `~/.archigraph/groups/<group>/domain.md`
- `~/.archigraph/groups/<group>/inventory.json`
- `~/.archigraph/groups/<group>/plan.json`
- The convention file named in the plan for this repo (under `conventions/`).
- `output-templates/overview.md` — fill this template, do not invent a new structure.

## Procedure

For each repo `<r>`:

### Step 1 — Confirm scope

Call `archigraph_whoami` with `cwd=<repo absolute path>` so subsequent calls scope correctly. Then `archigraph_stats(repo_filter=["<r>"])` to confirm the inventory numbers match.

### Step 2 — Identify the architectural skeleton

Call:

```
archigraph_find(question="entry points", repo_filter=["<r>"], depth=2, token_budget=600)
archigraph_find(question="public API surface", repo_filter=["<r>"], depth=2, token_budget=600)
archigraph_find(question="data model", repo_filter=["<r>"], depth=2, token_budget=600)
```

Use the convention file's `entry_points` section to know what "entry point" means for this stack. For example, in `django.md` it means URLConf root + `wsgi.py` + management commands; in `react.md` it means the router root + the top-level `App` component.

### Step 3 — Identify cross-repo edges

```
archigraph_cross_links(action=list, repo_filter=["<r>"], limit=20)
```

Mention any accepted cross-repo links in a section called "Connections to other repos". Pending candidates go in a "Pending links" callout; do not assert them as facts.

### Step 4 — Render

Open `output-templates/overview.md`, fill every section, write the result to `~/.archigraph/docs/<group>/<repo-slug>/overview.md`. Apply `_graph-searchability.md` and the stack convention strictly:

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
them to `~/.archigraph/groups/<group>/docgen-repairs.jsonl`, one JSON object
per line, and record the emission summary line in your pass report.

Common discoveries in this pass:
- Entry-point files that import modules whose stubs are unresolved in the graph.
- Cross-repo links whose direction or target is ambiguous in the graph but clear
  from the source you just read.

Use `source: "generate-docs/pass-3"` in every candidate emitted here.

### Step 7 — Save the result

Call:

```
archigraph_save_finding(
  question="What is the architectural overview of <repo>?",
  answer="<file: ~/.archigraph/docs/<group>/<repo-slug>/overview.md>",
  type="overview",
  repo_filter=["<r>"]
)
```

This creates a memory entry the future grooming agents can find by query.

When all repos are done, hand control back to the orchestrator.

---

**[pass-03 telemetry]** Print at end of this pass:
```
[pass-03] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
