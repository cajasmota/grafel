# Repair trust model

Companion to ADR-0015. Defines what the indexer accepts, rejects, and flags as suspicious when applying `repair.json`. The model is allowlist-only: the set of acceptable resolutions is closed, and adding new ones requires an ADR amendment.

## Allowlisted resolutions

Only the five `resolution` values from `repair-v1.schema.json` are accepted:

| Resolution | Required fields | Effect on endpoint |
|---|---|---|
| `bind_to_entity` | `target_entity_id` | Endpoint is rewritten to the named entity; disposition becomes `Resolved`. |
| `reclassify_as_external` | `module` | Endpoint becomes `ext:<module>`; disposition becomes `ExternalKnown`. |
| `reclassify_as_dynamic` | `dynamic_reason` | Endpoint is left as-is; disposition becomes `Dynamic`. The reason is stored on the edge. |
| `reclassify_as_resolved` | `new_target` | Endpoint is rewritten to `new_target`; resolver does NOT re-resolve. Use for cross-repo or runtime-known targets. |
| `abandon` | `abandon_reason` | Endpoint is dropped from the graph; counted in `repair_stats.json` under `abandoned`. |

Any other `resolution` value: **rejected** with `resolution_kind_unsupported`.

## Verification rules (run before apply)

A repair is **rejected** if any check fails. Rejection records the `edge_id` and reason code in `repair_stats.json` so the agent can re-attempt.

### R1 — `edge_id_unknown`
The `edge_id` does not match any current `repair_edge` candidate. Common cause: source moved between index runs (stale repair). The indexer does not auto-fix; the agent must reread `list_residuals` and submit a fresh repair.

### R2 — `target_entity_not_found`
Applies to `bind_to_entity`. The `target_entity_id` must exist in the current `graph.Document`. Lookup is exact; no fuzzy match.

### R3 — `self_loop_disallowed`
Applies to `bind_to_entity`. `target_entity_id == from_entity.id` is rejected. Even if the source genuinely calls itself, that loop is the responsibility of the static resolver, not the repair layer.

### R4 — `contradicts_contains_hierarchy`
Applies to `bind_to_entity`. The proposed target cannot be a `CONTAINS` ancestor of `from_entity` (a method cannot CALL/EXTENDS its own enclosing class via repair; static resolution handles that). This preserves the structural invariants emitted by Pass-3.

### R5 — `invalid_module_identifier`
Applies to `reclassify_as_external`. `module` must match `^[A-Za-z_][A-Za-z0-9_\-./]*$`. No leading `..`, no absolute paths, no shell metacharacters. This prevents path-traversal-style attacks via `ext:` prefixes that downstream tools might join with filesystem paths.

### R6 — `missing_required_field`
The `resolution` was named but its conditional required field was omitted.

### R7 — `reasoning_too_short`
`reasoning` is empty or whitespace-only. The reasoning string is preserved on the edge as `repair_reasoning` and shown to users in MCP query output, so it must be substantive (minimum 1 non-whitespace char enforced; reviewers should treat <10 chars as suspicious).

## Suspicious-but-accepted (flagged, not rejected)

These cases are accepted but emitted into `repair_stats.json` under `suspicious` so a human can audit:

- `confidence < 0.5` — accepted; agents are encouraged to abandon rather than low-confidence bind.
- `bind_to_entity` to an entity of a `kind` that does not normally receive the proposed `relation` (e.g. `CALLS` targeting a `SCOPE.Module` instead of a callable). The indexer doesn't reject because per-language kind/relation matrices are not fully canonical, but it flags.
- Multiple repairs converging on the same `target_entity_id` from a large fan-in (>50 distinct `from_entity` ids) — possible god-node mis-binding.

## Stale-repair detection

A repair becomes stale when its `edge_id` no longer matches any current `repair_edge` candidate. The indexer detects staleness by computing the set difference between repair `edge_id`s and current candidate `edge_id`s. Stale repairs:

- are **not** applied
- are **not** auto-deleted from `repair.json` (audit history; see open question 1 in ADR-0015)
- are listed in `repair_stats.json` under `stale_repairs[]` with their `edge_id`, `resolution`, and `resolved_at`

A subsequent `list_residuals` call returns the new candidate for that edge so the agent can re-submit.

## Source attribution (audit trail)

Every endpoint that the repair layer touches receives two properties before disposition classification:

- `resolved_by = "agent-repair"` — distinguishes from `"static"` (default) and any future sources.
- `repair_reasoning = "<the one-sentence string>"` — the verbatim `reasoning` from `repair.json`.

These properties survive into `graph.json` and are queryable through the existing MCP graph tools, so users can ask "which edges did the agent decide?" and see the reasoning per edge.

## Auditing requirements

- `repair_stats.json` MUST be emitted on every index run that read a `repair.json`, even if no repairs applied.
- The stats file MUST list, at minimum: `applied_count`, `rejected[]` (with reason codes), `stale[]`, `suspicious[]`, `total_residuals_before`, `total_residuals_after`, `bug_rate_before`, `bug_rate_after`.
- Sort order: `applied[]` and `stale[]` ordered by `edge_id` ascending; `rejected[]` and `suspicious[]` ordered by `edge_id` ascending. This preserves byte-identical-output determinism (ADR-0015, issue #486).

## Operator controls

- **Disable repair layer entirely:** `rm <repo>/.archigraph/repair.json`. Next index returns to pure-static behaviour.
- **Reject a specific repair manually:** delete its record from `repair.json` (or rewrite by hand) before reindex.
- **CI-only no-repair mode:** OPEN QUESTION in ADR-0015 — likely just file-absence; an `ARCHIGRAPH_DISABLE_REPAIR` env var is on the table if the file-absence convention proves insufficient.
