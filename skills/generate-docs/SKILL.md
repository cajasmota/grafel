# generate-docs

Generate module-organized markdown documentation for every repo in a registered archigraph group, then stitch it into a group-level synthesis with cross-repo links.

## When to use this skill

Invoke this skill when the user asks for any of:

- "Document this repo / this group."
- "Regenerate the docs after the recent refactor."
- "Write API reference / module guide / cross-repo overview."
- "Set up a VitePress doc site for the group."

Do not invoke it for one-off docstrings, README touch-ups, or commit-message writing. The skill assumes archigraph has already indexed the target repos (`archigraph index <repo>`) and that the repos are registered into a group.

## Inputs the skill expects

- A resolved archigraph group (the skill calls `whoami` first to confirm).
- Per-repo `<repo>/.archigraph/graph.json` produced by `archigraph index`.
- Group state under `~/.archigraph/groups/<group>/`.
- Optional cross-repo link state at `~/.archigraph/groups/<group>-links.json` and candidates at `~/.archigraph/groups/<group>-link-candidates.json`.
- Optional enrichment candidates at `<repo>/.archigraph/enrichment-candidates.json`.

If any of these are missing, the skill stops at Pass 0 and tells the user which `archigraph` CLI command to run first.

## Pass numbering (Pass 0 through Pass 9)

The skill is a strict pipeline. Each pass has a dedicated prompt file under `prompts/`. A subagent reads the prompt and follows it; the orchestrator (this skill) tracks progress and gates each pass on the previous one's output.

| Pass | Prompt | Purpose |
|------|--------|---------|
| 0 | `prompts/00-domain-qa.md` | First-run domain interview: what is this group, who owns it, what are the deployment boundaries. |
| 1 | `prompts/01-inventory.md` | Discover repos and entities via `search` / `graph_stats` / `list_clusters`. |
| 2 | `prompts/02-plan.md` | Produce a per-module documentation plan with token estimates. |
| 3 | `prompts/03-overview.md` | Repo-level `overview.md` for every repo. |
| 4 | `prompts/04-cluster.md` | Per-module deep-dive (parallel writer subagents, one per cluster). |
| 5 | `prompts/05-reference.md` | Reference docs: API, config, deployment, scripts, dependencies. |
| 6 | `prompts/06-cross-cutting.md` | Cross-cutting concerns: auth, logging, error handling, observability. |
| 7 | `prompts/07-group-synthesis.md` | Group-level synthesis page that ties the repos together. |
| 8 | `prompts/08-cross-link.md` | Validate links and resolve `list_link_candidates`. |
| 9 | `prompts/09-vitepress.md` | Optional VitePress site config. |

## archigraph MCP tool surface

The skill is built around the archigraph MCP server. The agent should call these tools directly (no shell-out to the `archigraph` CLI for read paths):

- `whoami` - resolve the group/repo for the caller.
- `search` - BM25-ranked query expanded by BFS; primary discovery tool.
- `describe` - look up an entity by id/qualified name/label.
- `related` - depth-bounded neighbor expansion.
- `trace` - confidence-weighted path (cross-repo aware).
- `list_clusters` - Louvain communities, used to seed module clustering in Pass 2.
- `get_source` - retrieve source-file snippet for a node.
- `recent_activity` - list entities whose source files changed since a timestamp.
- `save_finding` - persist a question/answer pair into the group memory directory.
- `list_link_candidates` / `resolve_link_candidate` - cross-repo link review (Pass 8).
- `list_enrichment_candidates` / `submit_enrichment` / `reject_enrichment` - close enrichment loops.
- `graph_stats` - corpus-level metrics (used in Pass 1 inventory).
- `get_telemetry` - server uptime and per-tool counters (debugging only).

### Calling conventions

- `repo_filter="<repo_slug>"` scopes a call to a single repo. Default behavior infers the repo from caller CWD via `whoami`.
- `repo_filter=null` (or omitted with `cwd` outside any registered repo) returns a summary across the whole group; use this for cross-group questions.
- `group=<name>` is only needed when the caller CWD is ambiguous or the user explicitly switched groups.
- Strip the `SCOPE.` prefix from any node-kind labels you print to the user (the schema uses `SCOPE.Component`, `SCOPE.Module`, etc., but agent-facing examples should say `Component`, `Module`).

## Output layout

For each repo `<r>` in the group, the skill writes into `<r>/docs/`:

```
docs/
  overview.md                  # Pass 3
  modules/
    <module-slug>/
      README.md                # Pass 4 (template: output-templates/module-readme.md)
      api.md                   # Pass 5
      flows.md                 # Pass 4
  reference/
    config.md
    deployment.md
    scripts.md
    dependencies.md
    misc.md
  how-to/
    local-dev.md
  glossary.md
```

Group-level output lands at `~/.archigraph/groups/<group>/docs/`:

```
docs/
  group-synthesis.md           # Pass 7
  cross-links.md               # Pass 8 summary
  vitepress.config.ts          # Pass 9 (optional)
```

## Conventions

The skill applies a stack-specific convention to every writer subagent. See `conventions/` for the registered conventions. Every convention requires `conventions/_graph-searchability.md` first, because that is the rule that makes documentation collide with code-symbol slugs in the graph (ADR-0007).

If the agent encounters a stack with no matching convention, it should stop and direct the user to run the `extend-convention` skill.

## Quality gates (snippets/verification-checklist.md)

Before any pass commits its output, the writer subagent runs the checks in `snippets/verification-checklist.md`. The orchestrator re-runs the same checklist before declaring the pass complete.

## Related

- `skills/extend-convention/SKILL.md` - companion skill for adding a new stack convention.
- ADR-0007 (`docs/adrs/0007-doc-as-bridge-for-cross-repo-and-dynamic-connections.md`) - why backticked code identifiers in headings matter.
- ADR-0008 - caller-CWD-aware routing, which is why `repo_filter` defaults work without the agent passing `cwd` explicitly.
