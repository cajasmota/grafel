# archigraph graph format

archigraph stores its code-knowledge graph on disk as a FlatBuffers binary
(`graph.fb`) defined in
[`internal/graph/schema/graph.fbs`](../internal/graph/schema/graph.fbs).
The design rationale is in
[ADR-0016](adrs/0016-binary-graph-format.md).

## Schema version field

The root `Graph` table carries a `version: int` field (currently `2`):

```
table Graph {
  version: int = 2;
  ...
}
```

This integer is the **wire format version** — it is independent of the
archigraph release version and of the MCP `wire_version` string.

### Versioning rules

| Scenario | Action |
|----------|--------|
| Additive field appended at the end of a table | No bump required — FlatBuffers default-fills missing fields on old readers. |
| Existing field semantics change, or a field is removed / reordered | Bump `version` by 1. |
| Breaking change incompatible with old readers | Bump `version` and add a migration path in `docs/migration/`. |

### Reader behaviour

When archigraph reads a `graph.fb` file it checks the `version` field:

- **Current version** — file is read normally.
- **Older version** — archigraph either migrates the data transparently (for
  additive-only changes) or returns a clear error asking the user to run
  `archigraph rebuild` to regenerate the graph.
- **Newer version** — archigraph returns an error asking the user to upgrade
  the binary.

### Current schema version history

| `version` | Introduced | Notes |
|-----------|------------|-------|
| 1 | v0.0.x | Initial FlatBuffers format (replaced legacy `graph.json`). |
| 2 | v0.1.0 | Pass 4 graph-algorithm fields added: `community_id`, `pagerank`, `centrality`, `is_god_node`, `is_surprise_endpoint`, `is_articulation_point`, `communities`, aggregate counters (#1620). Old `version=1` files read back with these fields zero/empty. |

## Field authorship rules

- Do **not** reorder existing fields — FlatBuffers assigns IDs by ordinal
  position.
- To deprecate a field, mark it `(deprecated)` in the `.fbs` file; do not
  remove it.
- New fields must be **appended** to the end of the table.

## Regenerating the Go bindings

```bash
flatc --go -o internal/graph/fbgraph internal/graph/schema/graph.fbs
```

Run this after any schema change and commit the updated `fbgraph/` files.
