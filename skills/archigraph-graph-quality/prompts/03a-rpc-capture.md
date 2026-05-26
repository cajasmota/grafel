# Phase 3a - MCP Daemon RPC Capture Protocol

Detailed daemon RPC capture procedure for per-question metrics collection during Phase 3 (with-MCP run).

## Daemon RPC capture (handler vs transport split)

**Why:** the daemon logs `[mcp-rpc] tool=<name> elapsed=<N>ms repo=<repo>` at `internal/daemon/mcp_rpc.go:193` for every RPC call dispatched. The `elapsed` value is the **handler** time — what the daemon spent actually computing the answer, exclusive of the JSON-RPC bridge transport. Wall-clock per-question time minus the sum of `elapsed` values is the **transport** time (bridge serialization, stdio pipe, host overhead).

This split is the whole point of the capture: a question with `wall=8000ms, handler_sum=7500ms` says the handler is the lever; a question with `wall=8000ms, handler_sum=400ms` says the transport is the lever.

## How to capture (Phase 3, step 7)

Use the deterministic CLI helper:

```
archigraph bench-capture rpc \
  --log ~/.archigraph/logs/daemon.log \
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
- `mcp_rpc_per_tool` — per-tool `{ "count": N, "sum_ms": M }` map.

## Log rotation

If the log shrank between step 3 and step 7, pass `--start-offset 0` and add a note in `notes`.

If `archigraph bench-capture rpc` fails (missing binary, permissions, sandbox), record `mcp_rpc_count: null` for every question and add an `"mcp_rpc_capture_error": "<reason>"` field. Phase 5 will surface the failure in the report rather than silently drop the split.
