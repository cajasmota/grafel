# Pass 5 — Reference

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

Reference docs are the dry, exhaustive, alphabetized pages. They live under `~/.grafel/docs/<group>/<repo-slug>/reference/` and are produced one section at a time, sequentially, by a single writer subagent per repo.

> **Pass 3a hook active.** Before writing any paragraph that describes an entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. Auto-repair residuals where unambiguous; otherwise emit the documented "Runtime-resolved edge" callout from that prompt. Do not silently drop unresolved outbound edges.

Sections (each is a separate file, each has a template):

- `api.md` — public API surface (`output-templates/api.md`)
- `config.md` — configuration & environment variables (`output-templates/reference-config.md`)
- `deployment.md` — how the repo deploys (`output-templates/reference-deployment.md`)
- `scripts.md` — CLI entry points, build scripts (`output-templates/reference-scripts.md`)
- `dependencies.md` — runtime + dev dependencies, version notes (`output-templates/reference-dependencies.md`)
- `misc.md` — anything stack-specific the convention demanded but didn't have a home (`output-templates/reference-misc.md`)

## Procedure (per repo)

### `api.md`

Public API includes: HTTP routes, gRPC services, exported functions/classes (per the convention's "public surface" rules), CLI commands, message-bus producers/consumers.

```
grafel_find(question="HTTP routes", repo_filter=["<r>"], depth=1, token_budget=900)
grafel_find(question="public exports", repo_filter=["<r>"], depth=1, token_budget=900)
grafel_find(question="CLI commands", repo_filter=["<r>"], depth=1, token_budget=600)
```

Also search for message-bus producers and consumers using the newer edge kinds:

```
grafel_find(question="message producers publishers Kafka", repo_filter=["<r>"], depth=1, token_budget=600)
grafel_find(question="message consumers subscribers Kafka", repo_filter=["<r>"], depth=1, token_budget=600)
grafel_find(question="queue broker RabbitMQ SQS", repo_filter=["<r>"], depth=1, token_budget=600)
```

For message-broker entities (`Queue` or `MessageTopic`), document them in the "Message bus" subsection of `api.md` rather than the HTTP-routes subsection. For each: entity name (backticked), kind (`Queue` for generic brokers like RabbitMQ/SQS, `MessageTopic` for Kafka topics), producers (via `PUBLISHES_TO` edges), consumers (via `SUBSCRIBES_TO` edges), and any stream transformations (via `TRANSFORMS` edges).

For each route/export, capture: name (in backticks), kind, file path, and a one-line purpose. Group by kind; sort alphabetically within each group.

### `config.md`

```
grafel_find(question="environment variables", repo_filter=["<r>"], depth=2, token_budget=900)
grafel_find(question="settings constants", repo_filter=["<r>"], depth=2, token_budget=900)
grafel_enrichments(action=list, repo_filter=["<r>"], kind="env-var")
```

If `grafel_enrichments(action=list)` returns blocking unknowns, list them in a "Known gaps" section. Do not fabricate values.

### `deployment.md`

Read the convention's `deployment_signals` section. For Django that means `wsgi.py`/`asgi.py`/Procfile/Dockerfile; for an infra-cdk repo it means stack files and synth output; for `infra-terraform.md` it means modules + backends.

```
grafel_find(question="deployment", repo_filter=["<r>"], depth=2, token_budget=800)
```

Cross-reference `domain.md` "Deployment" section to make sure you do not contradict it.

### `scripts.md`

Pull from `package.json` scripts (Node), `Makefile` targets, `manage.py` commands (Django), or whatever the convention names. Each script gets: name, command, purpose.

### `dependencies.md`

List direct dependencies only (no transitive). For each: name, version pin, purpose (one line). Pull from `package.json`, `pyproject.toml`, `go.mod`, etc., per the convention's `manifest_files` list.

### `misc.md`

Created only if the convention required it. Most repos won't have one.

## Verification, repair emission, and save

Run `snippets/verification-checklist.md` after each file.

After all six files are produced, run the emission step from
`snippets/docgen-repair-emission.md`. Reference documentation is a strong
source of the following discovery types:

- **`label_external`** — dependency listing (`dependencies.md`) frequently
  reveals external library stubs that the graph has not catalogued. For every
  direct dependency whose stub appears `UNRESOLVED` in the graph, emit a
  `label_external` candidate at confidence 0.92+ (you are reading the manifest
  directly).

  Example — from `dependencies.md` for a Node repo:

  ```json
  {
    "type": "label_external",
    "source_entity_id": "<entity id of the unresolved stub>",
    "target": "ext:stripe",
    "confidence": 0.93,
    "evidence": "package.json@line 12: \"stripe\": \"^14.0.0\" — confirmed external SaaS payment library",
    "source": "generate-docs/pass-5",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`resolve_ref`** — the route listing in `api.md` may name handler functions
  whose UNRESOLVED stubs become resolvable once you know the file they live in.

  Example — from `api.md` for a Django repo:

  ```json
  {
    "type": "resolve_ref",
    "source_entity_id": "<stub entity id>",
    "target": "<OrderViewSet entity id>",
    "confidence": 0.95,
    "evidence": "urls.py@line 47: path('orders/', OrderViewSet.as_view()) — stub resolved to OrderViewSet in orders/views.py",
    "source": "generate-docs/pass-5",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

Use `source: "generate-docs/pass-5"` in every candidate. Append to
`~/.grafel/groups/<group>/docgen-repairs.jsonl`.

Then save:

```
grafel_save_finding(
  question="What is the reference documentation for <repo>?",
  answer="<paths to reference/*.md>",
  type="reference",
  repo_filter=["<r>"],
)
```

When all repos in the group have completed reference docs, hand back to the orchestrator.

---

**[pass-05 telemetry]** Print at end of this pass:
```
[pass-05] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
