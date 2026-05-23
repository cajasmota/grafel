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
~/.archigraph/docs/<group>/<repo-slug>/cross-cutting/<topic>.md
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

**Anchor contract (`snippets/anchor-contract.md`).** Cross-cutting pages are
where the 2026-05-23 audit found the 17 anchor mismatches: stubs declared
`anchors: [summary, primary-implementation, patterns, consumers, gotchas]` but
the prose used `## Where it lives`, `## How it's used`. If you emit an
`anchors:` frontmatter list, **write the headings first, then derive `anchors:`
from those exact headings** — never the reverse. A declared anchor with no
matching heading in the same file is a hard failure. Apply
`snippets/link-hygiene.md` to every link you emit (no source-dir links, no bare
directory links).

### Step 5 — Aggregate to group level

After all per-repo files for the topic are written, write the group-level aggregator at `~/.archigraph/groups/<group>/cross-cutting/<topic>.md`. The aggregator is short — it points to each repo's page and calls out repo-to-repo divergence.

### Step 6 — Emit repair candidates

Run the emission step from `snippets/docgen-repair-emission.md`. Cross-cutting
passes cover auth, logging, observability, and error handling — topics that
routinely expose two discovery types:

- **`resolve_ref` / `add_edge`** — auth middleware and logging utilities are
  called from many modules; their dynamic-dispatch patterns (decorator
  registration, middleware chaining, signal hooks) are often invisible to the
  static extractor.

  Example — from `auth.md` for a Django repo:

  ```json
  {
    "type": "add_edge",
    "source_entity_id": "<JWTAuthMiddleware entity id>",
    "target": "TokenValidator",
    "edge_kind": "CALLS",
    "confidence": 0.85,
    "evidence": "middleware.py@line 34: self._validator.validate(token) — TokenValidator instantiated in __init__, not visible at call site",
    "source": "generate-docs/pass-6",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`label_external`** — logging and observability sections frequently name
  external SDKs (Datadog, Sentry, OpenTelemetry) whose stubs are unresolved.

  Example — from `observability.md`:

  ```json
  {
    "type": "label_external",
    "source_entity_id": "<stub entity id>",
    "target": "ext:datadog-lambda",
    "confidence": 0.91,
    "evidence": "tracing.go@line 9: import \"github.com/datadog/datadog-lambda-go\" — Datadog Lambda tracer SDK",
    "source": "generate-docs/pass-6",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

Append to `~/.archigraph/groups/<group>/docgen-repairs.jsonl`.
Use `source: "generate-docs/pass-6"` in every candidate.

### Step 7 — Verification

Run `snippets/verification-checklist.md` for each file produced.

When every topic is done across every repo, hand back to the orchestrator.
