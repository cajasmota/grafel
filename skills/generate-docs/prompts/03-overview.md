# Pass 3 — Repo Overview

Write `<repo>/docs/overview.md` for every repo in the group. The overview is the entry point a new engineer reads first; it is also the page Pass 7 quotes when synthesizing the group-level page.

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

Open `output-templates/overview.md`, fill every section, write the result to `<repo>/docs/overview.md`. Apply `_graph-searchability.md` and the stack convention strictly:

- Every code identifier in headings goes in backticks: `` ## `OrderViewSet` ``.
- Every code block has a language tag.
- Module names listed in the overview should match the slugs in `plan.json` exactly so Pass 4's deep-dives are reachable from the overview by relative link.

### Step 5 — Verification

Run `snippets/verification-checklist.md`. If any check fails, fix and re-run before moving on.

### Step 6 — Save the result

Call:

```
archigraph_save_finding(
  question="What is the architectural overview of <repo>?",
  answer="<file: <repo>/docs/overview.md>",
  type="overview",
  repo_filter=["<r>"]
)
```

This creates a memory entry the future grooming agents can find by query.

When all repos are done, hand control back to the orchestrator.
