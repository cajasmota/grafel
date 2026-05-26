# FlatBuffers-Generated Go Bindings

**Files in this directory are GENERATED** by the FlatBuffers compiler (`flatc`) from the schema source.

## Do Not Edit These Files

The `.go` files in this directory (e.g., `Entity.go`, `Graph.go`, `Community.go`, `Relationship.go`, `PropertyEntry.go`) are **automatically generated**. Any hand-edited changes will be **silently lost** the next time `make fbgen` is run.

## Schema Source

The authoritative schema is:

```
internal/graph/schema/graph.fbs
```

## How to Regenerate

To regenerate all bindings after modifying the `.fbs` schema:

```bash
make fbgen
```

Internally, this invokes:

```bash
flatc --go -o internal/graph/fbgraph internal/graph/schema/graph.fbs
```

Requires `flatc` (install via `brew install flatbuffers` on macOS, or your platform's package manager).

## Adding Documentation

### Option 1: Schema Comments (Preferred)

Add comments directly to the `.fbs` source file to document entities and fields:

```fbs
table Entity {
  // Unique identifier for this entity
  id: string (key);
  
  // Human-readable name
  name: string;
}
```

These comments will be preserved through regeneration and appear in the FlatBuffers documentation.

### Option 2: Sibling Non-Generated Files

If extensive documentation is needed that does not belong in the schema itself, create a sibling file in this directory:

- `fbgraph_design.md` — design rationale, usage patterns, performance notes
- Entity-specific docs: `Entity_design.md`, etc.

These files are **not** regenerated and will persist indefinitely.

## References

- **FlatBuffers ADR**: `docs/adrs/0016-binary-graph-format.md`
- **Schema**: `internal/graph/schema/graph.fbs`
- **Makefile target**: `make fbgen` (see `Makefile` for full details)
