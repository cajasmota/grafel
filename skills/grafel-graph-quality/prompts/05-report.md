# Phase 5 - Report generation

Render the final shareable markdown report from the four JSON artifacts in the run directory. Write it to `--output` (default `~/private/benchmarks/mcp-quality-bench-<YYYY-MM-DD>.md`) and also copy it to `<run-dir>/report.md`.

## Inputs

- `questions.json`
- `with-mcp.json` (or `with-mcp-iter-<N>.json` files if `--iterations > 1`)
- `without-mcp.json` (same)
- `judgment.json`
- Optional `--baseline <path>` - prior report markdown for regression diffing.

## Report structure

```markdown
# Grafel MCP Quality Benchmark - <YYYY-MM-DD>

**Group:** `<group>`
**Repos:** `<repo-1>`, `<repo-2>`
**Run directory:** `~/.grafel/quality-check/<timestamp>/`
**Iterations:** `<N>` (median/stddev reported when N > 1)
**Token source:** `host usage_info` (or `estimated (host did not surface usage)`)
**Focus:** `<category or "all">`

## Test set
`<N>` questions across categories: entity-lookup (x), reference-finding (x), cross-stack-tracing (x), pattern-discovery (x), architecture-overview (x), subsystem-deep-dive (x), specific-trace (x), data-access (x), http-cross-repo (x).

Skipped categories: `<list with reasons>`.

## Token economy

| Method | Input tokens | Cache-read tokens | Output tokens | Total (input+output) | Per-question median |
|---|---:|---:|---:|---:|---:|
| With MCP | ... | ... | ... | ... | ... |
| Without MCP (grep+read) | ... | ... | ... | ... | ... |
| **Ratio MCP / grep (lower=MCP wins)** | ... | | ... | ... | ... |

Lower-is-better ratios highlighted: **bold** if MCP wins, *italic* if grep wins, plain if tie.

### Cost projection
At this token rate, a 1000-query session would cost roughly:
- With MCP: ~`<X>` tokens
- Without MCP: ~`<Y>` tokens
- Delta: `<Z>` tokens saved (or burned) by using the MCP

## Speed

| Method | Median per-question (ms) | P95 (ms) | Total wall-clock (ms) | Median tool calls per question |
|---|---:|---:|---:|---:|
| With MCP | ... | ... | ... | ... |
| Without MCP | ... | ... | ... | ... |

### Handler vs Transport (with-MCP only)

The daemon logs handler-side latency per RPC at `internal/daemon/mcp_rpc.go:193`
(`[mcp-rpc] tool=<X> elapsed=<N>ms`). Phase 3 captures every such line by
running `grafel bench-capture rpc --start $START --end $END` (#2298) and
persists `mcp_rpc_count`, `mcp_rpc_handler_ms_sum`, `mcp_rpc_handler_ms_p50`,
`mcp_rpc_handler_ms_p99` in `with-mcp.json` (field definitions: `schema/with-mcp-artifact.schema.json`, #2302).
This subsection splits the with-MCP wall-clock budget into handler time (work
inside the daemon) and transport time (everything else — JSON-RPC bridge
marshal, stdio pipe, host overhead, agent thinking between tool calls).

| Metric | Value |
|---|---:|
| Total with-MCP wall-clock (ms) | `<sum of wall_clock_ms across questions>` |
| Total handler time (ms) | `<sum of mcp_rpc_handler_ms_sum>` |
| Total transport + agent gap (ms) | `<wall − handler>` |
| Handler share of wall | `<handler / wall × 100>%` |
| Transport share of wall | `<transport / wall × 100>%` |
| Median per-call handler ms (p50) | `<median of mcp_rpc_handler_ms_p50>` |
| Worst per-call handler ms (p99) | `<max of mcp_rpc_handler_ms_p99>` |

| # | Question | Wall (ms) | RPC calls | Handler sum (ms) | Transport (ms) | Handler share |
|---|---|---:|---:|---:|---:|---:|
| q01 | ... | ... | ... | ... | ... | ...% |

**Interpretation guide** (do not omit — paste verbatim into the report):

- If **handler share ≥ 70%**, the lever is the handler path (graph traversal, marshaling inside the tool). Adjacency-indexing (#2285) and single-marshal (#2287) are the right next moves.
- If **handler share ≤ 30%**, the lever is the bridge: a batch macro (#2286), keeping the daemon hot, or trimming per-call agent overhead.
- If the split is **between 30% and 70%**, both levers matter; prioritise by absolute ms saved, not percentage.

If `mcp_rpc_count` is `null` for any question (capture failure), call this out in
a "Capture gaps" callout under this table and exclude those questions from the
totals.

## Quality

| Method | Full | Partial | Wrong | Unknown | Avg confidence | Net quality (full=1, partial=0.5, wrong=-0.5, unknown=0) |
|---|---:|---:|---:|---:|---:|---:|
| With MCP | ... | ... | ... | ... | ... | ... |
| Without MCP | ... | ... | ... | ... | ... | ... |

## Per-question breakdown

| # | Category | Question | With-MCP tokens | Without-MCP tokens | Token ratio | With-MCP quality | Without-MCP quality |
|---|---|---|---:|---:|---:|---|---|

Token ratio formatted as `0.42×` (MCP saved 58%) or `2.10×` (MCP burned 110% more).

## Findings

### Where MCP wins
- `<question id + concrete example>` - bullet describing the win with the cite to the per-question row.

### Where MCP loses
- `<question id + concrete example>` - what MCP missed or burned tokens on, plus the visible root cause if any (e.g., "grafel_search returned 0 hits because the entity name uses a non-ASCII suffix").

### Surprising patterns
- Anything that does not fit the above two buckets.

## Issues encountered

For each `tool_calls[].ok = false` in `with-mcp.json` or notes about malformed data:

- **Tool errors** - `<tool>` returned `<error>` on `<question id>`.
- **Malformed responses** - `<tool>` returned data missing field `<x>` on `<question id>`.
- **Tool description quality** - questions where the agent picked the wrong tool first; suggests the description is unclear.
- **Cost-model mismatches** - queries that took >2× the median time. Hint at a slow tool path.

## Recommendations

Concrete tuning ideas for the grafel coordinator. Each recommendation cites the question(s) that motivated it.

- **Tool description for `grafel_<X>`** - rewrite to clarify `<Y>` (cites: q03, q07).
- **Add cache for `<call pattern>`** - the agent ran the same call N times on q05; consider memoization.
- **Pattern discovery weak on `<kind>`** - q08 missed obvious recurrence; suggests Phase 4 of ADR-0018 needs `<X>`.
- **Doc-gen flow should leverage `<MCP capability>`** - q02 showed a 6× token saving for reference finding; docgen should call this tool directly instead of round-tripping through `grafel_search`.

### Anti-patterns to avoid
- `<X>` - observed in q04, costs N tokens per occurrence.

## Extraction calibration

(Appended by Phase 6 - `prompts/06-extraction-calibration.md`. Leave this heading as the insertion point; Phase 6 fills the over/under table, calibration verdict, and prune/add recommendations. Omit only if `--no-calibration`.)

## Regression diff (if --baseline provided)

| Question | Prior with-MCP tokens | Current | Δ | Prior quality | Current | Δ |

Summary deltas: total token change, quality change, new failure modes.

## Raw data appendix

Full per-question dump for verification:

### q01 - `<question>`
- **Category:** `<>`
- **Anchors:** `<>`
- **Expected signals:** `<>`
- **With-MCP answer:** `<verbatim answer text>`
- **With-MCP tool calls:** `<list>` (`<count>` total)
- **With-MCP metrics:** input=`<>`, output=`<>`, cache=`<>`, wall=`<>`ms, conf=`<>`
- **Without-MCP answer:** `<verbatim answer text>`
- **Without-MCP tool calls:** `<list>` (`<count>` total)
- **Without-MCP metrics:** input=`<>`, output=`<>`, cache=`<>`, wall=`<>`ms, conf=`<>`
- **Ground truth:** `<summary>`
- **Judgment - with-MCP:** `<score>` - rationale: `<>` - misses: `<>` - extras: `<>`
- **Judgment - without-MCP:** `<score>` - rationale: `<>` - misses: `<>` - extras: `<>`

(repeat for each question)

## Methodology notes

- Token counts sourced from the host's `usage_info` per message. When the host did not surface usage, char/4 estimation was used and the table header reads "(estimated)".
- Ground truth established by an independent grep+read pass before either run's answer was opened.
- The judge used `rg` / `Read` (not the MCP) so it does not favor either side.
- No source-code content was logged in any intermediate JSON or in this report - only paths, kinds, line numbers, and the agent's prose answer.
- The skill ran against the user's existing grafel daemon; no daemon was spawned by the skill.
```

## Generation rules

- **Always compute the cost projection**, even when only one iteration ran.
- **Always render the per-question breakdown** - this is the table the user copies to the coordinator.
- For `--iterations N > 1`, report median ± stddev in the token / speed tables, and use the median in the per-question breakdown.
- When `--baseline` is provided, render the regression diff section; otherwise omit it (do not show empty placeholders).
- Find the run dir's `iter-*.json` files if `--iterations > 1` and aggregate before rendering.

## Privacy

- Quote the agent's answers verbatim in the appendix (the user wrote them and is sharing voluntarily).
- Do not include source-code snippets - paths, lines, kinds only.
- Do not name competitor tools anywhere - use "predecessor MCP tool" or "Tool A" if you must reference the earlier benchmark context.

## Output

Write the report to `--output` (default `~/private/benchmarks/mcp-quality-bench-<YYYY-MM-DD>.md`, creating parent directories if missing) and copy it to `<run-dir>/report.md`. Print the absolute output path and a one-line scoreboard: `with-MCP: <full/partial/wrong/unknown>, without-MCP: <full/partial/wrong/unknown>, token ratio: <X>×`. Return control to the orchestrator.
