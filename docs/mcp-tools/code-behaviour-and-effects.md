# MCP tools — Code behaviour & effects

[← Back to the MCP tools index](../mcp-tools.md)

Per-function effects, control flow, purity, intra-procedural data flow, and template literals.

---

## `archigraph_effects`

Effects + sinks for a function.

Key parameters: `entity_id` (required), `include` (`branches`/`effect_contexts` — adds cond/loop + complexity), `repo_filter[]`.

Output: detected effects and sinks; optional branch and effect-context detail.

---

## `archigraph_control_flow`

On-demand per-function control-flow graph + complexity.

Key parameters: `entity_id` (required), `detail` (`outline`/`decisions`/`data`/`full`), `repo_filter[]`.

Output: CFG at the requested detail level with cyclomatic complexity.

---

## `archigraph_pure_functions`

Functions with no detected effects — memoization candidates.

Key parameters: `repo_filter[]`, `limit` (default 200).

Output: list of pure functions.

---

## `archigraph_data_flows`

Request-input → sink DATA_FLOWS_TO edges.

Key parameters: `entity_id`, `sink_kind`, `repo_filter[]`, `limit` (default 100).

Output: data-flow edges carrying `field`, `sink_kind`, and `hop_path`.

---

## `archigraph_def_use`

Intra-procedural def-use chains (last-write-wins) per function.

Key parameters: `entity_id`, `repo_filter[]`, `limit` (default 50).

Output: per-function def-use chains.

---

## `archigraph_template_patterns`

i18n / log_format / sql template literals lifted per file.

Key parameters: `kind`, `repo_filter[]`, `limit` (default 200).

Output: template-literal records grouped by kind.
