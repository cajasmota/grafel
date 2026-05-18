# MCP tool surface — residual repair

This document specifies the MCP tools introduced by ADR-0015. Tools are registered alongside the existing surface in `internal/mcp/tools.go` and the registration table in `internal/mcp/server.go` (compare the `query_graph` / `get_node` / `submit_resolution` family — these new tools follow the same conventions).

All tool input/output schemas are JSON Schema Draft-07.

---

## `list_residuals`

Paginates `repair_edge` candidates from `<repo>/.archigraph/enrichment-candidates.json`. The cursor is stateless and deterministic (it's the last `edge_id` returned), so an agent can resume between sessions without server-side state.

### Input

```json
{
  "type": "object",
  "required": ["repo"],
  "additionalProperties": false,
  "properties": {
    "repo": {
      "type": "string",
      "description": "Repo identifier or absolute path. Resolved against the caller-CWD routing rules from ADR-0008."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 200,
      "default": 50
    },
    "cursor": {
      "type": ["string", "null"],
      "description": "edge_id from the last item of the previous page. Null/absent for first page."
    },
    "disposition_filter": {
      "type": "array",
      "items": { "type": "string", "enum": ["bug-extractor", "bug-resolver"] },
      "description": "Optional. Restrict to a subset of bug dispositions. Default = both."
    },
    "priority": {
      "type": "string",
      "enum": ["centrality_desc", "edge_id_asc"],
      "default": "edge_id_asc",
      "description": "Stable ordering of returned items. centrality_desc requires Pass-4 graph attributes; falls back to edge_id_asc if unavailable."
    }
  }
}
```

### Output

```json
{
  "type": "object",
  "required": ["items", "next_cursor", "remaining"],
  "properties": {
    "items": {
      "type": "array",
      "items": { "$ref": "enrichment-candidates-v2.schema.json#/definitions/Candidate" }
    },
    "next_cursor": {
      "type": ["string", "null"],
      "description": "Pass back as `cursor` to fetch the next page. Null when the list is exhausted."
    },
    "remaining": {
      "type": "integer",
      "minimum": 0,
      "description": "Number of repair_edge candidates left after this page."
    }
  }
}
```

### Errors

- `repo_not_indexed` — `.archigraph/enrichment-candidates.json` missing. Suggest the user run `archigraph index <repo>` first.
- `schema_version_unsupported` — file present but `schema_version != 2`.
- `invalid_cursor` — cursor does not refer to a known `edge_id`.

---

## `submit_repair`

Validates a proposed repair against the trust model (see `repair-trust-model.md`) and appends or replaces a record in `<repo>/.archigraph/repair.json`. Writes are atomic (temp file + rename). Last-writer-wins by `resolved_at` if two calls race on the same `edge_id`.

### Input

```json
{
  "type": "object",
  "required": ["repo", "edge_id", "resolution", "confidence", "reasoning"],
  "additionalProperties": false,
  "properties": {
    "repo": { "type": "string" },
    "edge_id": { "type": "string", "pattern": "^er:[0-9a-f]{16}$" },
    "resolution": {
      "type": "string",
      "enum": ["bind_to_entity", "reclassify_as_external", "reclassify_as_dynamic", "reclassify_as_resolved", "abandon"]
    },
    "target_entity_id": { "type": "string" },
    "module":           { "type": "string" },
    "new_target":       { "type": "string" },
    "dynamic_reason":   { "type": "string" },
    "abandon_reason":   { "type": "string" },
    "confidence":       { "type": "number", "minimum": 0, "maximum": 1 },
    "reasoning":        { "type": "string", "minLength": 1, "maxLength": 500 }
  }
}
```

(Conditional `required` fields by `resolution` mirror `repair-v1.schema.json`.)

### Output

```json
{
  "type": "object",
  "required": ["ok"],
  "properties": {
    "ok": { "type": "boolean" },
    "written": {
      "type": "object",
      "description": "Present iff ok == true. The exact record persisted to repair.json (timestamped server-side).",
      "$ref": "repair-v1.schema.json#/definitions/Repair"
    },
    "rejected_reason": {
      "type": "string",
      "description": "Present iff ok == false. Stable error code from the trust-model reject list.",
      "enum": [
        "edge_id_unknown",
        "target_entity_not_found",
        "self_loop_disallowed",
        "contradicts_contains_hierarchy",
        "invalid_module_identifier",
        "missing_required_field",
        "resolution_kind_unsupported",
        "reasoning_too_short"
      ]
    }
  }
}
```

### Errors (transport-level, distinct from `rejected_reason`)

- `repo_not_indexed` — same as above.
- `repair_file_corrupt` — existing `repair.json` cannot be parsed. Agent should surface to the user.
- `io_error` — write failed (disk full, permission denied, etc.).

---

## `reindex` (optional, Phase 1 stretch)

Reuses the indexer entry point used by `cmd/archigraph/index.go` to let the agent verify bug-rate movement after a batch of `submit_repair` calls.

### Input

```json
{
  "type": "object",
  "required": ["repo"],
  "properties": { "repo": { "type": "string" } }
}
```

### Output

```json
{
  "type": "object",
  "required": ["bug_rate_before", "bug_rate_after", "applied", "rejected", "stale"],
  "properties": {
    "bug_rate_before": { "type": "number" },
    "bug_rate_after":  { "type": "number" },
    "applied":         { "type": "integer" },
    "rejected":        { "type": "integer" },
    "stale":           { "type": "integer" }
  }
}
```

`bug_rate_before` is read from the previous run's `disposition_breakdown` (`cmd/archigraph/index.go:220`); `bug_rate_after` is the freshly-indexed value.

---

## Agent loop (reference)

```text
loop:
  page = list_residuals(repo, limit=50, cursor=last_cursor)
  for c in page.items:
    decision = reason_over(c.context_window, c.candidates, c.extracted_metadata)
    submit_repair(repo, edge_id=c.context.edge_id, **decision)
  if page.next_cursor is None: break
  last_cursor = page.next_cursor
reindex(repo)   # optional: confirm bug_rate dropped
```

This loop is the centerpiece of the v1.0 demo (ADR-0015 Phase 2).
