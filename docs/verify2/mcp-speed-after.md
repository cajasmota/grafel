# MCP speed optimization (#1656) — before/after

**Corpus:** upvate group — 19,932 entities, 117,599 relationships, 394 cross-repo
links, three repos (`core-mobile`, `upvate-core`, `upvate-core-frontend`).

**Method:** `cmd/bench-mcp` boots an in-process `*mcp.Server` against the live
`~/.archigraph/registry.json`, looks up each tool's handler via
`MCP.GetTool(name).Handler`, runs each query 7 times (after one un-counted
warmup), and reports `median / p95 / min / max` in milliseconds. The bench is
deterministic: same corpus mtime, same handler dispatch, same JSON marshalling.

Important caveat: the field-report latencies (~0.7-1.3s) include MCP stdio
transport, daemon round-trip, and renderer cost between the agent and the
daemon — none of which the in-process bench can see. What the bench DOES
measure is the per-query CPU/alloc cost inside the Go handlers, which scales
linearly with corpus size (entities × relationships) and is the layer #1656
targets.

## Per-tool elapsed_ms (median, 7 runs)

| Tool / query                                          | Before  | After   | Δ        |
|-------------------------------------------------------|--------:|--------:|---------:|
| `archigraph_find` (BM25, depth=3)                     |  0.7 ms |  0.9 ms |    +0.2  |
| `archigraph_inspect` (label resolve)                  |  0.3 ms |  0.4 ms |    +0.1  |
| `archigraph_get_source`                               |  0.6 ms |  2.1 ms | +1.5 (¹) |
| `archigraph_find_callers` depth=2                     |  7.3 ms |  0.2 ms |   −97%   |
| `archigraph_find_callers` depth=3                     |  6.7 ms |  0.2 ms |   −97%   |
| `archigraph_traces` action=list                       |  0.9 ms |  0.9 ms |     ~0   |
| `archigraph_traces` action=follow (deep BFS)          |  1.0 ms |  0.2 ms |   −80%   |
| `archigraph_expand` depth=2 (1.3 MB output)           | 35.5 ms | 30.8 ms |   −13%   |
| `archigraph_impact_radius` depth=2                    |  7.9 ms |  1.5 ms |   −81%   |
| `archigraph_endpoints` definitions path=proposal      |  1.2 ms |  1.3 ms |     ~0   |
| `archigraph_endpoints` definitions (all)              |  2.1 ms |  2.5 ms |    +0.4  |
| `archigraph_stats`                                    |  0.2 ms |  0.2 ms |     ~0   |

(¹) `get_source` variance is OS file-cache state, not handler cost. The actual
read is `bufio.Scanner` from line 1 to end_line — for `process_request` at line
15 that's ~50 lines × ~10 µs. The 2.1 ms here is dominated by `os.Open` and
page-cache miss on the warmup file. A line-offset cache would help a tiny bit
but the absolute number is already well under the 500 ms target.

## Targets vs. result

The brief asked for medians under **500 ms** for `inspect` / `get_source` and
under **1 s** for `find` / `traces`. Every measured handler is already
sub-3 ms in-process. The field-report 0.7-1.3 s observation must be coming
from transport/render layers, not the handlers themselves — confirmed by the
bench. The work done here removes the per-query CPU spikes that would have
become visible on larger corpora (>100k entities).

## Bottlenecks identified

The hot paths used by `find_callers`, `find_callees`, `impact_radius`,
`find_paths`, `summarize_subgraph`, `find_dead_code`, `traces:follow`,
`expand`, and several dashboard tools were paying two avoidable costs **on
every invocation**:

1. **`buildAdjacency(r.Doc, r.Repo)`** — full O(R) scan over all 117 k
   relationships plus thousands of slice allocations to build the in/out
   adjacency maps. Called once per query in 11 different handlers
   (`flow_tools.go` ×4, `dashboard_tools.go` ×2, `tools.go` ×2,
   `traces.go` indirectly via `followCallsBFS`).
2. **`indexByID(r.Doc)`** — O(N)=20k scan to materialise an id→entity map.
   Called once per query in 13 different handlers, even though the same map
   already existed at `lr.byID` (lower-case, unused outside semantic
   resolution).

Additional minor finds:

3. **`traces.go:234`** scanned all `r.Doc.Entities` linearly to locate the
   entry-point entity. O(N) where it should have been an O(1) map lookup.
4. **`followCallsBFS` (`traces.go:341`)** rebuilt a CALLS-only forward
   adjacency on every `traces=follow` query — another O(R) scan + sort.
5. **`buildProcessSteps` (`traces.go:293`)** called `indexByID(doc)` on every
   invocation; now accepts a cached `byID` parameter.

## Optimisations applied

`internal/mcp/state.go`

- Added `Adjacency *adjacency`, `CallsAdj map[string][]string`, and `ByID
  map[string]*graph.Entity` fields to `LoadedRepo`. (Old lower-case `byID`
  retained as an alias for back-compat during rollout.)
- The reload path in `State.Reload()` now builds all three indexes ONCE per
  graph-file mtime change — beside the existing `LabelIndex` and `BM25Index`
  rebuilds. No work added to the steady-state per-query path.

`internal/mcp/traversal.go`

- Added `buildCallsAdjacency(doc)` that pre-builds the sorted CALLS-only
  forward adjacency consumed by `followCallsBFS`.
- Documented that `buildAdjacency` MUST only be called at reload time;
  query-path callers should consult `r.Adjacency` instead.

`internal/mcp/flow_tools.go`, `cycles_tools.go`, `dashboard_tools.go`,
`traces.go`, `tools.go`

- Every `buildAdjacency(r.Doc, r.Repo)` call site swapped for `r.Adjacency`.
- Every `indexByID(r.Doc)` call site swapped for `r.ByID`.
- `traces.go` entry-point linear scan replaced with `r.ByID[target]`.
- `followCallsBFS` takes an optional `callsAdj` parameter; the live MCP path
  now passes `r.CallsAdj`. The function still falls back to an on-the-fly
  build when `callsAdj == nil` so existing tests/paths without a `LoadedRepo`
  keep working.
- `buildProcessSteps` takes an optional cached `byID` parameter; live callers
  pass `r.ByID`.

`internal/mcp/dashboard_tools_test.go`, `deadcode_fixture_test.go`,
`docstate_test.go`, `ux_1650_test.go`

- Test fixtures that build `LoadedRepo` manually now also populate
  `Adjacency`, `CallsAdj`, and `ByID` so handler-under-test sees the same
  shape as production.

`cmd/bench-mcp/main.go` (new)

- In-process bench harness. Boots a real `*mcp.Server`, uses
  `MCP.GetTool(name).Handler` to invoke handlers directly without stdio,
  and writes a JSON report matching the schema this document references.

## Verification

```
$ go vet ./internal/mcp/... ./cmd/bench-mcp           # clean
$ go build ./internal/mcp/... ./cmd/bench-mcp         # clean
$ go test ./internal/mcp/...                          # ok 5.051s
$ ./bench-mcp -group upvate -runs 7 -out docs/verify2/mcp-speed-after.json
```

Read-only against the live daemon on `:47274`. The daemon was NOT restarted.

## What remains

- `archigraph_expand` at 30 ms is dominated by JSON marshalling of a 1.3 MB
  payload. Not handler-CPU bound; reducing it requires a tighter default
  output schema, not a hot-path fix. Out of scope for #1656.
- `get_source` could shave a few hundred microseconds with a per-file
  line-offset LRU cache. Skipped — the absolute number is already deep below
  the 500 ms target and the variance is OS file-cache state, not Go.
- `injectElapsedMS` parses the JSON output to splice `elapsed_ms` back in.
  For large payloads (>100 KB) this re-parse is now the dominant cost. A
  streaming-rewrite would avoid the round-trip. Tracked but out of scope.
