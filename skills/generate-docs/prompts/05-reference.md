# Pass 5 — Reference

Reference docs are the dry, exhaustive, alphabetized pages. They live under `<repo>/docs/reference/` and are produced one section at a time, sequentially, by a single writer subagent per repo.

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
archigraph_find(question="HTTP routes", repo_filter=["<r>"], depth=1, token_budget=900)
archigraph_find(question="public exports", repo_filter=["<r>"], depth=1, token_budget=900)
archigraph_find(question="CLI commands", repo_filter=["<r>"], depth=1, token_budget=600)
```

Also search for message-bus producers and consumers using the newer edge kinds:

```
archigraph_find(question="message producers publishers Kafka", repo_filter=["<r>"], depth=1, token_budget=600)
archigraph_find(question="message consumers subscribers Kafka", repo_filter=["<r>"], depth=1, token_budget=600)
archigraph_find(question="queue broker RabbitMQ SQS", repo_filter=["<r>"], depth=1, token_budget=600)
```

For message-broker entities (`Queue` or `MessageTopic`), document them in the "Message bus" subsection of `api.md` rather than the HTTP-routes subsection. For each: entity name (backticked), kind (`Queue` for generic brokers like RabbitMQ/SQS, `MessageTopic` for Kafka topics), producers (via `PUBLISHES_TO` edges), consumers (via `SUBSCRIBES_TO` edges), and any stream transformations (via `TRANSFORMS` edges).

For each route/export, capture: name (in backticks), kind, file path, and a one-line purpose. Group by kind; sort alphabetically within each group.

### `config.md`

```
archigraph_find(question="environment variables", repo_filter=["<r>"], depth=2, token_budget=900)
archigraph_find(question="settings constants", repo_filter=["<r>"], depth=2, token_budget=900)
archigraph_enrichments(action=list, repo_filter=["<r>"], kind="env-var")
```

If `archigraph_enrichments(action=list)` returns blocking unknowns, list them in a "Known gaps" section. Do not fabricate values.

### `deployment.md`

Read the convention's `deployment_signals` section. For Django that means `wsgi.py`/`asgi.py`/Procfile/Dockerfile; for an infra-cdk repo it means stack files and synth output; for `infra-terraform.md` it means modules + backends.

```
archigraph_find(question="deployment", repo_filter=["<r>"], depth=2, token_budget=800)
```

Cross-reference `domain.md` "Deployment" section to make sure you do not contradict it.

### `scripts.md`

Pull from `package.json` scripts (Node), `Makefile` targets, `manage.py` commands (Django), or whatever the convention names. Each script gets: name, command, purpose.

### `dependencies.md`

List direct dependencies only (no transitive). For each: name, version pin, purpose (one line). Pull from `package.json`, `pyproject.toml`, `go.mod`, etc., per the convention's `manifest_files` list.

### `misc.md`

Created only if the convention required it. Most repos won't have one.

## Verification & save

Run `snippets/verification-checklist.md` after each file. After all six are produced, save:

```
archigraph_save_finding(
  question="What is the reference documentation for <repo>?",
  answer="<paths to reference/*.md>",
  type="reference",
  repo_filter=["<r>"],
)
```

When all repos in the group have completed reference docs, hand back to the orchestrator.
