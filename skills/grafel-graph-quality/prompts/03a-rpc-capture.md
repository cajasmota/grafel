# Phase 3a - MCP Daemon RPC Capture Protocol

Detailed daemon RPC capture procedure for per-question metrics collection during Phase 3 (with-MCP run).

## Daemon RPC capture (handler vs transport split)

**Why:** the daemon logs `[mcp-rpc] tool=<name> elapsed=<N>ms wire_bytes=<B> payload_token_estimate=<T> repo=<repo>` at `internal/daemon/mcp_rpc.go:MCPToolCall` for every RPC call dispatched (the `wire_bytes`/`payload_token_estimate` fields were added in #2828). The `elapsed` value is the **handler** time — what the daemon spent actually computing the answer, exclusive of the JSON-RPC bridge transport. Wall-clock per-question time minus the sum of `elapsed` values is the **transport** time (bridge serialization, stdio pipe, host overhead).

This split is the whole point of the capture: a question with `wall=8000ms, handler_sum=7500ms` says the handler is the lever; a question with `wall=8000ms, handler_sum=400ms` says the transport is the lever.

## How to capture (Phase 3, step 7)

Use the deterministic CLI helper:

```
grafel bench-capture rpc \
  --log ~/.grafel/logs/daemon.log \
  --start-offset $START \
  --end-offset $END
```

`$START` = `log_start_offset` snapshotted in step 3 (before the first MCP call).
`$END` = current log file size at step 7 (question end). The CLI reads the exact byte window, parses `[mcp-rpc] … elapsed=<N>ms` lines, and emits JSON ready to merge into `metrics`. Parsing regex, percentile math, and null rules are all encapsulated in the CLI (#2298). Do not re-implement them here.

## Field semantics

**Canonical definition in `schema/with-mcp-artifact.schema.json`:**

- `mcp_rpc_count` — number of `elapsed=` lines in the window.
- `mcp_rpc_handler_ms_sum` — sum of all elapsed_ms values.
- `mcp_rpc_handler_ms_p50` — median handler duration; `null` when count = 0.
- `mcp_rpc_handler_ms_p99` — 99th-percentile handler duration; `null` when count = 0.
- `mcp_rpc_per_tool` — per-tool `{ "count": N, "sum_ms": M, "sum_bytes": B, "sum_token_est": T }` map.
- `mcp_rpc_wire_bytes_sum` — sum of the final on-wire tool-result payload size (bytes) across the window, measured daemon-side in `daemonMCPCallTool` **after `applyIDInterning`** (#2828).
- `mcp_rpc_payload_token_estimate_sum` — char/4 estimate of those bytes. Approximate (host tokenizer differs) — use as a **relative lever-finder**, not exact reconciliation against billed `input_tokens`.

**Reading the split:** `mcp_rpc_payload_token_estimate_sum` is the cost the daemon *produced* (handler payload). The host's billed `input_tokens` is what the model *ingested*. The delta is transport/host overhead. A handler whose `sum_bytes` dominates is the target for the optimization menu (top-N defaults, terse formats, token budgets).

**Backward compatibility:** old daemon logs lack `wire_bytes`/`payload_token_estimate`. The parser still counts those lines for ms; the byte/token sums simply stay `0`. A `0` (not `null`) byte sum on a non-zero count therefore means "legacy log slice", not "zero-byte payloads".

## Log rotation

If the log shrank between step 3 and step 7, pass `--start-offset 0` and add a note in `notes`.

If `grafel bench-capture rpc` fails (missing binary, permissions, sandbox), record `mcp_rpc_count: null` for every question and add an `"mcp_rpc_capture_error": "<reason>"` field. Phase 5 will surface the failure in the report rather than silently drop the split.
