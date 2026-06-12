# MCP tools — Findings & docs

[← Back to the MCP tools index](../mcp-tools.md)

Group memory store, the agent-learned pattern store, and the docgen staging → promote workflow.

---

## `archigraph_save_finding`

Persist a Q&A pair to the group memory store.

Key parameters: `question` (required), `answer` (required); optional `type`, `nodes[]`, `repo_filter[]`.

Output stored at `~/.archigraph/findings/<group>/`.

---

## `archigraph_list_findings`

Read back saved findings.

Key parameters (optional): `since` (RFC3339), `entity_id`, `limit`.

Output: stored findings, optionally filtered.

---

## `archigraph_patterns`

Agent pattern store (ADR-0018) — distinct from the indexer-extracted [`archigraph_graph_patterns`](cross-cutting-analysis.md#archigraph_graph_patterns).

Key parameters: `action` (required: `query`/`record`), `text` (query text), `category`, `limit` (default 10), `steps[]`, `exemplars[]`.

Output: matched patterns (`query`) or a write acknowledgement (`record`).

---

## docgen workflow

Standard flow: `start_run` → write files into `staging_path` → `validate` → `promote`. Use `abort` to reset a failed run and `status` to check progress mid-run.

### `archigraph_docgen_start_run`

Start or resume a docgen staging run for a group.

Key parameters: `group` (required), `resume` (default `true`), `no_git` (default `false`).

Output: `run_id` + `staging_path`.

### `archigraph_docgen_status`

Inspect an in-flight docgen run.

Key parameters: `run_id` (required), `no_git` (default `false`).

Output: files written + SHA-256 per file.

### `archigraph_docgen_validate`

Validate a staging run (frontmatter + cross-links). Read-only, no file writes.

Key parameters: `run_id` (required), `no_git` (default `false`).

### `archigraph_docgen_promote`

Atomic promote: staging → canonical. Blocks SSG scaffolding. Rotates the previous canonical set.

Key parameters: `run_id` (required), `force` (default `false`).

### `archigraph_docgen_abort`

Abort a staging run: `rm -rf` staging and release the per-group lock. Canonical is left untouched.

Key parameters: `run_id` (required).

### `archigraph_docgen_list`

List canonical doc files for a group under `~/.archigraph/docs/<group>/`.

Key parameters: `group` (required).

---

## `archigraph_apply_docgen_repairs`

Docgen feedback: apply repair candidates to graph enrichments in a single batch.

Key parameters: `repo_filter[]`, `dry_run` (bool).

---

## `archigraph_apply_doc_semantics`

Doc L2: apply agent-produced DesignDecision nodes + RATIONALE_FOR edges to the graph.

Key parameters: `repo_filter[]`, `dry_run` (bool).
