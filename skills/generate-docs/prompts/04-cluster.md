# Pass 4 — Per-module deep-dive

For every entry under `plan.passes.4_cluster.modules`, produce a self-contained module page (or set of pages). This pass runs writer subagents **in parallel** — one subagent per module — bounded by a configured concurrency limit.

## Inputs (per writer subagent)

- The single module entry from `plan.json` you are responsible for.
- The convention file named in that entry (under `conventions/`).
- `conventions/_graph-searchability.md` — universal; read first.
- `output-templates/module-readme.md` and `output-templates/flows.md`.
- `~/.archigraph/groups/<group>/domain.md`.

## Output

Per module:

```
<repo>/docs/modules/<module-slug>/
  README.md      # always (output-templates/module-readme.md)
  flows.md       # if the module has runtime flows worth diagramming
  api.md         # only if the module is the primary owner of a public API; otherwise Pass 5 owns api.md at the repo level
```

## Procedure

### Step 1 — Pull the module subgraph

Use the community ids in your plan entry:

```
search(
  question="<module title> architecture",
  repo_filter=["<repo>"],
  depth=3,
  token_budget=<plan.token_budget>,
)
```

Then for each of the top-5 entities in the module:

```
related(node="<entity>", depth=2, repo_filter=["<repo>"])
```

These neighbors are what you describe in the module README's "Key entities" section.

### Step 2 — Find dynamic edges

The graph cannot see runtime-only couplings. The convention file lists places where these typically live for the stack (e.g., for `django.md`: signal connections, middleware, async tasks). For each, ask:

```
search(
  question="<dynamic-edge-pattern>",
  repo_filter=["<repo>"],
  depth=2,
  token_budget=400,
)
```

When you find one, name both ends of the connection in a backticked heading inside `flows.md` so the slug-collision rule (ADR-0007) bridges them in the graph. Example:

```markdown
## How `OrderCreated` reaches `BillingService`
```

### Step 3 — Pull source where needed

Within your `source_snippets` budget, use `get_source(node_id=..., context_lines=20)` for entities whose intent is unclear from name + neighbors alone. Quote at most ~10 lines per snippet in the doc; reference the file path in backticks.

### Step 4 — Render

Fill the templates strictly. Do not invent extra top-level sections. The conventions file may add stack-specific sections (e.g., Django adds a "Migrations" section); follow the convention but do not stray.

### Step 5 — Cross-link

If your module references entities owned by another module, link to that module's `README.md`. If those references cross repo boundaries, render them per `snippets/cross-link-format.md` instead.

### Step 6 — Verification

Run every check in `snippets/verification-checklist.md`. The orchestrator will reject your output otherwise.

### Step 7 — Save

```
save_finding(
  question="What does the <module-slug> module do in <repo>?",
  answer="<file: <repo>/docs/modules/<module-slug>/README.md>",
  type="module",
  nodes=["<top-entity-1>", "<top-entity-2>"],
  repo_filter=["<repo>"],
)
```

Hand control back to the orchestrator. The orchestrator joins all writer subagents and starts Pass 5 only when every module has produced verified output.
