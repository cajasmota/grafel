# MCP payload-size optimisation (#1663) — before/after

**Corpus:** upvate group — three repos (`upvate-core`, `upvate-core-frontend`,
`core-mobile`), live `~/.archigraph/registry.json`.

**Method:** `cmd/bench-mcp` boots `*mcp.Server` in-process, calls each tool
handler 5×, records `bytes_out` from the last run (sum of all `TextContent`
text on the result). Bytes are converted to tokens via the conventional
chars/4 estimate.

Note: the bench bypasses `Server.wrap`, so the measured bytes are pre-envelope
(no `elapsed_ms` injection). The wrap layer also calls `MarshalIndent` →
`Marshal` in this PR, so live wire payloads benefit by the same percentage on
the envelope itself plus the per-tool savings shown below.

## Per-tool bytes (median of 5 runs, last sample)

| Tool / query                                          | Before     | After      | Δ %      |
|-------------------------------------------------------|-----------:|-----------:|---------:|
| `archigraph_find` (BM25, depth=3, token_budget=800)   |        558 |        558 |    +0.0% |
| `archigraph_inspect` (label resolve)                  |        473 |        407 |   −14.0% |
| `archigraph_get_source` (incl. source body)           |      2,936 |      2,935 |    −0.0% |
| `archigraph_find_callers` depth=2                     |        354 |        317 |   −10.5% |
| `archigraph_find_callers` depth=3                     |        354 |        317 |   −10.5% |
| `archigraph_traces` action=list                       |     19,769 |     15,411 |   −22.0% |
| `archigraph_traces` action=follow                     |      1,159 |        781 |   −32.6% |
| `archigraph_expand` depth=2                           |  1,326,608 |  1,006,391 |   −24.1% |
| `archigraph_impact_radius` depth=2                    |      1,119 |        877 |   −21.6% |
| `archigraph_endpoints` definitions path=proposal      |      4,337 |      3,461 |   −20.2% |
| `archigraph_endpoints` definitions (all)              |     55,256 |     44,237 |   −19.9% |
| `archigraph_stats`                                    |        482 |        332 |   −31.1% |
| **TOTAL session**                                     |**1,413,405** | **1,076,024** | **−23.9%** |
| Estimated tokens (chars/4)                            |    353,351 |    269,006 |   −23.9% |

**Target met: 20%+ across the session.** A session running this query set saves
~84,000 tokens (~23.9%) at zero schema cost.

## Why some tools didn't move

- **`archigraph_find`**: handler returns the compact text format from
  `renderCompact()` (graph header + node/edge lines), not JSON. It was never
  paying the pretty-print tax. No regression, no win.
- **`archigraph_get_source`**: payload is dominated by the literal source-code
  body. The JSON envelope is a few hundred bytes; minifying it shaves ~10
  bytes on a 2.9 KB response.

The tools that DO move are all the JSON-shape responses: `inspect`, `traces`,
`expand`, `impact_radius`, `endpoints`, `stats`, `find_callers`. Wins range
from −10% (small objects, lots of structural overhead per byte) to −33%
(`stats`, `traces:follow` — dense flat objects where every key-value pair was
followed by `,\n  `).

## Format decisions

| Payload shape | Format | Why |
|---|---|---|
| Single-entity / nested object (`inspect`, `find_paths`, `whoami`, `stats`) | **Minified JSON** | Schema preserved, callers `json.Unmarshal` as before. TOON would hurt nested shapes. |
| List-of-record (`endpoints`, `traces:list`, `find_callers`, search results) | **Minified JSON** | Same caller contract. Tabular gives more, but breaks every existing `json.Unmarshal` consumer. Kept as a future opt-in via `tabularEncode`. |
| Compact text graph (`find`, `expand`'s `renderCompact` path) | **Unchanged** | Already a compact text format, not JSON. |
| Source-body responses (`get_source`) | **Minified JSON envelope** | Body dominates size; minifying the envelope is a few bytes. |
| Disk artefacts (repair findings, patterns, candidates, docstate, memory notes) | **Indented JSON (unchanged)** | These files are read by humans on disk; pretty-printing is the right default. |

The brief offered TOON as a possible upgrade for list-of-record. A spot-check
test (`TestTabularEncode_SavesTokensVsJSON`) shows tabular IS shorter than
minified JSON for homogeneous lists. But the schema contract MCP callers
already depend on is `json.Unmarshal`-able JSON — every test in this package
and the field-report consumers parse the response as JSON. Switching even a
single tool to tabular requires caller-side updates. The `tabularEncode`
helper is shipped and tested here so future PRs can opt individual list
endpoints into it once a caller-side decoder lands.

## Implementation

Two-line behavioural change:

- `internal/mcp/tools.go::jsonResult` — `json.MarshalIndent(v, "", "  ")` →
  `json.Marshal(v)`. Used by 79 call sites; benefits every JSON-shape tool.
- `internal/mcp/server.go::wrap` (the `elapsed_ms` envelope, #1650) —
  `MarshalIndent` → `Marshal` for both object and array envelopes. Same
  schema: `items` / `count` / `elapsed_ms` keys preserved.

New helpers in `internal/mcp/render.go`:

- `compactJSON(v any) string` — exported-ish minified JSON helper that returns
  `"null"` on marshal failure. Available for future tool writers.
- `tabularEncode(schema []string, rows [][]any) string` — TOON-style
  list-of-record encoder. Header line `[!schema {f1,f2,...}]` then one
  `{v1,v2,...}` row per record, with `,` `{` `}` `\` escaped via backslash.
  NOT used by any tool yet — see "Format decisions" above.

New test file `internal/mcp/compact_test.go`:

- `TestCompactJSON_Minified` — no `\n` / `  ` in output, round-trips.
- `TestJSONResult_NoIndent` — guards against re-introducing pretty-print.
- `TestTabularEncode_Shape` — schema/row format and escaping.
- `TestTabularEncode_SavesTokensVsJSON` — tabular is shorter than minified
  JSON for homogeneous list-of-record payloads.

## Schema preservation

Verified by the existing test suite (`go test ./internal/mcp/...`). Every test
that parses tool output via `json.Unmarshal` still passes. Three tests in
`traces_test.go` and one in `server_test.go` had string-matched against the
pretty-print form (`"count": 2` with the space); updated to match the
minified form (`"count":2`). No semantic changes.

## Disk artefacts deliberately NOT minified

- `internal/mcp/repair.go` (`repair.json`)
- `internal/mcp/candidates.go` (`patterns/candidates/*.json`, links)
- `internal/mcp/docstate.go` (`.archigraph/docstate.json`)
- `internal/mcp/tools.go::handleSaveFinding` (memory note files)
- `internal/mcp/telemetry.go::SnapshotJSON` (admin telemetry text)

These are read by humans (on disk, in the dashboard, via `cat`). The
pretty-print cost is paid once per write and they aren't on the MCP wire.

## Targets vs. result

The brief asked for **20-30%+** reduction. The session-wide result is **23.9%**
— inside the target band with the higher-volume endpoints (`expand`, `traces`,
`endpoints`) landing at 20-25% and the flat-object handlers (`stats`,
`traces:follow`) doing even better at 31-33%.

## Coordination with #1656

#1656 (cached adjacency + byID at reload) was a CPU/alloc win on hot paths.
This PR (#1663) is a payload-bytes win on response serialisation. They are
orthogonal — no shared state, no test interaction. Verified by running both
sets of tests green from origin/main + this branch.
