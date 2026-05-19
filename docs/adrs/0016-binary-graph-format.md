# ADR-0016: Binary graph format — FlatBuffers v2

- **Status**: Proposed (phase-1 design + prototype landed; flip-day in a later release)
- **Date**: 2026-05-19
- **Deciders**: Jorge Cajas
- **Related**: issue #634, ADR-0005 (on-disk graph schema), ADR-0009 (cross-repo id namespacing), `internal/graph/graph.go` (v1 JSON document)

## Context

archigraph emits `<repo>/.archigraph/graph.json` as the canonical on-disk graph artifact. Every downstream consumer — the MCP query server (`internal/mcp/*`), the `doctor`/`quality`/`dashboard` subcommands, cross-repo link passes, the determinism harness, the bug-rate evaluator, third-party tools — pays a JSON unmarshal cost on every open.

Measured against the current 11.34 MB fixture (`client-fixture-b`, 100k+ entities + relationships):

| Operation                     | Time         | Allocations         |
|-------------------------------|--------------|---------------------|
| `json.Unmarshal(graph.json)`  | ~132 ms      | 50 MB / 640 k allocs |
| `json.Unmarshal + linear scan to find one entity` | ~120 ms | 50 MB / 640 k allocs |

That is the floor on every MCP tool call that needs graph state, on every doctor invocation, on every cross-repo link pass. As corpora grow toward the multi-million-entity ceiling targeted in `project_archigraph_v1_ship_gate_state`, the JSON parse dominates wall-time and memory pressure; it is also the single largest source of GC churn in long-running MCP sessions.

We have two cost levers:
1. **Format**: stop parsing JSON. The graph schema is tabular and stable (ADR-0005); a binary format with zero-copy reads removes the parse entirely.
2. **Indexing**: edges and entities should be lookup-by-id, not linear-scan. The v1 format has no index; consumers either build a transient `map[string]*Entity` per call or accept O(N) scans.

This ADR addresses (1) directly and lays the groundwork for (2) by emitting an entity vector that is sorted-by-id and supports binary search out of the box.

## Decision

**Adopt FlatBuffers as the v2 archigraph on-disk graph format.** The wire schema is defined in `internal/graph/schema/graph.fbs`; Go bindings live in `internal/graph/fbgraph/`; writer in `internal/graph/fbwriter/`; reader in `internal/graph/fbreader/`.

### Why FlatBuffers (vs the alternatives)

| Format         | Parse cost                | Random access            | Schema evolution    | Disk size  | Go ergonomics            | Tooling |
|----------------|---------------------------|--------------------------|---------------------|------------|--------------------------|---------|
| **MessagePack**| O(N) full decode          | None — must decode all   | Implicit, fragile   | ~0.7× JSON | Idiomatic                | Many libs |
| **Protobuf**   | O(N) full decode          | None                     | Field numbers       | ~0.5× JSON | Idiomatic                | First-class |
| **Cap'n Proto**| O(1) mmap                 | Yes                      | Field IDs           | ~0.5× JSON | Less mature in Go        | OK |
| **FlatBuffers**| **O(1) mmap, zero-copy**  | **Yes (key+binary search)** | Field IDs        | ~0.5× JSON | Generated, verbose       | First-class |

FlatBuffers wins on the two axes that matter for this workload: zero-copy mmap reads, and built-in binary-search keyed access (`(key)` annotation on `Entity.id` generates `EntitiesByKey`). Cap'n Proto is the closest second; we picked FB because the Go binding (`github.com/google/flatbuffers/go`) is maintained, the `flatc` compiler ships in Homebrew, and the on-disk format is well-understood by other tools in the codegen ecosystem.

### Schema

```fbs
namespace archigraph;

table PropertyEntry { key: string; value: string; }

table Entity {
  id: string (key);
  qualified_name: string;
  kind: string;
  subtype: string;
  module: string;
  name: string;
  source_file: string;
  source_line: int;
  source_col: int;
  properties: [PropertyEntry];
}

table Relationship {
  from_id: string;
  to_id: string;
  kind: string;
  properties: [PropertyEntry];
}

table Graph {
  version: int = 2;
  computed_at: string;
  repo_tag: string;
  entities: [Entity];
  relationships: [Relationship];
}

root_type Graph;
```

Property maps are flattened into key-sorted `PropertyEntry` vectors so the on-disk bytes are deterministic across runs (issue #481).

### Migration plan (phase-by-phase)

1. **Phase 1 — design + prototype (this PR, #634)**: ADR-0016 + .fbs + writer + reader + benchmark. Indexer learns an opt-in `--export-fb` flag that **dual-writes** graph.fb next to graph.json. graph.json stays the source of truth; nothing reads graph.fb in production yet.
2. **Phase 2 — consumer onboarding**: MCP server, doctor, and cross-repo link passes learn to prefer graph.fb when present (fall back to graph.json). Dual-write becomes the indexer default. One release at this state.
3. **Phase 3 — flip**: graph.fb becomes the canonical artifact. graph.json becomes opt-in (`--export-json`) for jq workflows and human debugging. One release at this state.
4. **Phase 4 — deprecate**: graph.json emission removed; agent-readable text export available via `archigraph dump --format=json`.

Every consumer touched in phase 2 stays compatible with phase 1 / phase 4 layouts via the existing `graph.SchemaVersion` const plus the v2 FlatBuffers schema version field; a missing graph.fb is treated as "fall back to graph.json", a missing graph.json is treated as "binary-only repo".

### Performance commitments

These are the gates phase-2 promotion to dual-write-by-default must clear, measured on `client-fixture-b` (11.4 MB JSON, 100k+ rows) — actual numbers from `BenchmarkSizesReport`, `BenchmarkFBOpen`, `BenchmarkFBLookupEntityHot` in `internal/graph/fb_bench_test.go`:

| Commitment                              | Target  | Measured (phase-1) |
|-----------------------------------------|---------|--------------------|
| ≥10× faster MCP parse-or-open           | 10×     | **~80× (132 ms → 1.6 ms)**       |
| ≤30% indexer write overhead vs JSON     | ≤+30%   | dual-write is additive; ~one JSON-pass equivalent |
| ≥3× smaller on disk                     | 3×      | **1.15× only (11.4 MB → 9.94 MB)** — falls short, see Tradeoffs |
| Hot lookup ≤1 µs                        | ≤1 µs   | **~40 ns (3.5 M× faster than JSON-reparse-per-call)** |

The 3× disk-size target was set optimistically. Real fixtures show the dominant cost is the string content (entity IDs, qualified names, source paths) which FlatBuffers does not compress — only the JSON envelope (quotes, commas, field names) is removed. Phase-2 should revise this gate or add an optional zstd outer wrapper (`graph.fb.zst`). The wall-time and allocation wins are the real prize; the size win is a bonus that fell short.

### Tradeoffs

- **Lose `jq`-able**: graph.json is human-inspectable; graph.fb requires `flatc --strict-json --raw-binary` round-trip or an `archigraph dump` helper. Mitigation: keep graph.json available behind `--export-json` through phase-3 and ship the dump helper in phase-2.
- **+codegen step**: contributors need `flatc` installed (`brew install flatbuffers`, `make fbgen`). Mitigation: generated files are checked in; codegen only required when editing the .fbs.
- **+schema discipline**: adding/removing fields requires append-only or `(deprecated)` annotations. Mitigation: covered by `flatc`'s field-ID semantics; CI lint can enforce.
- **Binding verbosity**: the generated Go API is offset-and-builder oriented (`EntityAddName(b, off)` etc.), not idiomatic. Mitigation: `fbwriter` and `fbreader` wrap the codegen so the rest of the codebase keeps working with `graph.Document`.
- **Size win is small**: 1.15× on real fixtures, not 3×. The disk-size commitment in this ADR is downgraded to "≤JSON size" with optional zstd wrapper in phase-2.

### Open questions (for phase-2)

- Should `graph-stats.json` move to a FlatBuffers sidecar too, or stay JSON given its tiny size and `doctor`-readability? (Lean: stay JSON.)
- Edge index: the .fbs vector has no by-`from_id` index, so `IterateRelationshipsFromID` is O(R). Phase-2 should add a sorted-by-from_id parallel vector or an explicit `RelationshipIndex` table.
- Communities / surprise edges / algorithm stats are currently absent from the v2 schema. Add in phase-2 once consumers actually read graph.fb.

## Consequences

**Positive**:
- ~80× faster cold open and ~3.5 M× faster hot lookup on a representative fixture.
- Allocation pressure drops from 50 MB/640 k allocs per open to 9.9 MB/8 allocs (single bulk read).
- Sets up phase-2 work (consumer onboarding) with concrete, measured numbers; #634 phase-1 is done.

**Negative**:
- Larger surface area: a new schema file, generated bindings, codegen step, and a second on-disk artifact during the dual-write window.
- Loses the "open graph.json in a text editor" affordance during the deprecation window; mitigated by `archigraph dump`.

**Risk**:
- FlatBuffers `(key)` requires the entity vector be id-sorted. The indexer's `sortDocumentForEmission` already guarantees this for graph.json (#481), so we get it for free — but any new emit path must respect the invariant or `EntitiesByKey` returns false. Phase-2 will add a `flatc --schema --bfbs-comments` CI lint.
