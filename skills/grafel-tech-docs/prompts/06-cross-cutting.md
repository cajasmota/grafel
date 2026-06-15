# Pass 6 — Cross-cutting concerns

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
~/.grafel/docs/<group>/<repo-slug>/cross-cutting/<topic>.md
```

Group-level aggregator (Pass 7 will use these to fill the synthesis page):

```
~/.grafel/groups/<group>/cross-cutting/<topic>.md
```

## Procedure (per topic)

### Step 1 — Collect signals across all repos

```
grafel_find(question="<topic>", repo_filter=null, depth=2, token_budget=1200)
```

Setting `repo_filter=null` is intentional — this is a cross-group question and you want the summary-first behavior.

### Step 2 — Drill per repo

For each repo `<r>`:

```
grafel_find(question="<topic>", repo_filter=["<r>"], depth=2, token_budget=800)
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

After all per-repo files for the topic are written, write the group-level aggregator at `~/.grafel/groups/<group>/cross-cutting/<topic>.md`. The aggregator is short — it points to each repo's page and calls out repo-to-repo divergence.

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

Append to `~/.grafel/groups/<group>/docgen-repairs.jsonl`.
Use `source: "generate-docs/pass-6"` in every candidate.

### Step 7 — Verification

Run `snippets/verification-checklist.md` for each file produced.

When every topic is done across every repo, hand back to the orchestrator.

---

**[pass-06 telemetry]** Print at end of this pass:
```
[pass-06] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
