# Pass 6 — Cross-cutting concerns

Some topics span every module: authentication, authorization, logging, error handling, observability, feature flags, rate limiting. They deserve their own pages so readers don't have to reconstruct them from per-module docs.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

Default topics (override per group via `plan.passes.6_cross_cutting.topics`):

- `auth.md` — authentication and authorization
- `logging.md` — log conventions, structured fields, log shipping
- `errors.md` — error taxonomy, retry/dead-letter behavior, user-facing errors
- `observability.md` — metrics, tracing, dashboards, alert routing

Each topic is one writer subagent. They run in parallel.

## Output

Per repo per topic:

```
<repo>/docs/cross-cutting/<topic>.md
```

Group-level aggregator (Pass 7 will use these to fill the synthesis page):

```
~/.archigraph/groups/<group>/cross-cutting/<topic>.md
```

## Procedure (per topic)

### Step 1 — Collect signals across all repos

```
archigraph_find(question="<topic>", repo_filter=null, depth=2, token_budget=1200)
```

Setting `repo_filter=null` is intentional — this is a cross-group question and you want the summary-first behavior.

### Step 2 — Drill per repo

For each repo `<r>`:

```
archigraph_find(question="<topic>", repo_filter=["<r>"], depth=2, token_budget=800)
```

### Step 3 — Resolve the slug-collision targets

Cross-cutting headings should name the central code identifier in backticks. For example, in `auth.md`:

```markdown
## How `JWTAuthMiddleware` validates tokens
```

This makes the cross-cutting page a bridge node in the graph between `JWTAuthMiddleware` (code) and any other doc that mentions it.

### Step 4 — Write per-repo file

Use whichever output template fits, or write freeform if no template applies (cross-cutting topics are too varied to template uniformly). Always:

- Backticks on identifiers in headings.
- Language tags on code blocks.
- A "Where this is enforced" section listing file paths.
- A "Where this is bypassed" section if the convention's `cross_cutting_pitfalls` lists known gotchas (e.g., management commands that skip middleware).

### Step 5 — Aggregate to group level

After all per-repo files for the topic are written, write the group-level aggregator at `~/.archigraph/groups/<group>/cross-cutting/<topic>.md`. The aggregator is short — it points to each repo's page and calls out repo-to-repo divergence.

### Step 6 — Verification

Run `snippets/verification-checklist.md` for each file produced.

When every topic is done across every repo, hand back to the orchestrator.
