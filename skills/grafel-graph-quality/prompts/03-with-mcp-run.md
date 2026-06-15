# Phase 3 - With-MCP run

Answer every question from `questions.json` using **only grafel MCP tools**. Record full per-question metrics. Your output is `with-mcp.json` in the run directory.

> **Context isolation:** This phase runs in a FRESH subagent context. You have NOT seen the grep-only results from Phase 2. Do not open `without-mcp.json` during this phase. This ordering is intentional: grep-only runs first (Phase 2) so that run cannot be contaminated by MCP results; you run second and must not look back.

## Allowed tools

You may call any grafel MCP tool: `grafel_whoami`, `grafel_search`, `grafel_describe`, `grafel_related`, `grafel_trace`, `grafel_list_clusters`, `grafel_get_source`, `grafel_recent_activity`, `grafel_list_link_candidates`, `grafel_list_enrichment_candidates`, `grafel_graph_stats`, `grafel_patterns`, `grafel_get_telemetry`, etc.

You may **not** call `rg`, `grep`, `Read` on source files, or any other non-MCP file-inspection tool. If a question is unanswerable through MCP alone, record `confidence: 0.0` and mark `unknown: true` - that is a valid result.

## Per-question protocol

For each question in `questions.json`:

1. Note the host's `usage_info` snapshot at question start (input/output/cache tokens emitted so far in this session).
2. Note `wall_clock_start` (monotonic time, RFC3339 nanos).
3. **Snapshot the daemon log byte-offset.** Read the current size of `~/.grafel/logs/daemon.log` (`stat -f%z` on macOS, `stat -c%s` on Linux) and remember it as `log_start_offset` (= `$START`). If the file does not exist (rare; daemon may have just started and not yet flushed), set `log_start_offset = 0` and proceed.
4. Answer the question using grafel MCP tools. Take as many tool calls as needed but stop when you reach a defensible answer.
5. Note `wall_clock_end`.
6. Note the host's `usage_info` at question end.
7. **Capture daemon RPC metrics.** Note the current log size as `$END`, then run:
   ```
   grafel bench-capture rpc --start $START --end $END
   ```
   Merge the resulting JSON directly into this question's `metrics` block (`mcp_rpc_count`, `mcp_rpc_handler_ms_sum`, `mcp_rpc_handler_ms_p50`, `mcp_rpc_handler_ms_p99`, `mcp_rpc_per_tool`, `mcp_rpc_wire_bytes_sum`, `mcp_rpc_payload_token_estimate_sum`). The two byte/token sums are the **daemon-side payload cost** (final on-wire result size, measured after id-interning); comparing `mcp_rpc_payload_token_estimate_sum` against this question's billed `input_tokens` splits handler-payload cost from model-ingestion/transport overhead (#2828). If the command fails (missing binary, permissions, sandbox), set `mcp_rpc_count: null` and add `"mcp_rpc_capture_error": "<reason>"` to the artifact. See "Daemon RPC capture" below for the field semantics.
8. Compute the delta and record the metrics below.

## Daemon RPC capture

See `prompts/03a-rpc-capture.md` for the detailed daemon RPC capture protocol, including the log-offset snapshot procedure, CLI helper invocation, field semantics, and error handling.

## Output schema (`with-mcp.json`)

```json
{
  "version": 1,
  "method": "with-mcp",
  "iteration": 1,
  "started_at": "<RFC3339>",
  "ended_at": "<RFC3339>",
  "results": [
    {
      "id": "q01",
      "answer": "<the agent's prose answer>",
      "confidence": 0.85,
      "unknown": false,
      "tool_calls": [
        {"tool": "grafel_search", "args_digest": "sha256:...", "ok": true, "ms": 142}
      ],
      "tool_call_count": 4,
      "tools_used": ["grafel_search", "grafel_describe", "grafel_related"],
      "metrics": {
        "input_tokens": 12345,
        "output_tokens": 678,
        "cache_read_tokens": 5000,
        "cache_creation_tokens": 0,
        "wall_clock_ms": 8421,
        "mcp_rpc_count": 4,
        "mcp_rpc_handler_ms_sum": 7980,
        "mcp_rpc_handler_ms_p50": 1850,
        "mcp_rpc_handler_ms_p99": 2410,
        "mcp_rpc_per_tool": {
          "grafel_search": {"count": 2, "sum_ms": 3600, "sum_bytes": 4096, "sum_token_est": 1024},
          "grafel_describe": {"count": 1, "sum_ms": 2120, "sum_bytes": 1536, "sum_token_est": 384},
          "grafel_related": {"count": 1, "sum_ms": 2260, "sum_bytes": 2048, "sum_token_est": 512}
        },
        "mcp_rpc_wire_bytes_sum": 7680,
        "mcp_rpc_payload_token_estimate_sum": 1920
      },
      "notes": "Mentioned grafel_search returned 0 hits initially; widened query."
    }
  ]
}
```

## Token accounting

The host (Claude Code) provides `usage_info` per message. The total tokens for a question are the sums **across all agent messages emitted while answering it**:

- `input_tokens` += `usage_info.input_tokens + usage_info.cache_creation_input_tokens`
- `output_tokens` += `usage_info.output_tokens`
- `cache_read_tokens` += `usage_info.cache_read_input_tokens`

If the host does not surface `usage_info`, fall back to `len(text) / 4` for input and output respectively, and set `"estimated": true` on each result. Phase 5 will label the report accordingly.

## Honesty rules

- Do not retry indefinitely to "win" a question. Stop when you have a defensible answer or after a reasonable effort.
- Record tool failures verbatim in `tool_calls[].ok = false` with the error string in `notes`. This data is used to surface MCP failure modes in the report.
- If a tool returned partial/malformed data, note that in `notes`. Phase 5's "Issues encountered" section depends on this.
- Confidence should reflect honest uncertainty, not match the user's expected outcome.

## Privacy

- The `answer` field may include entity names, file paths, line numbers, kinds, and structural facts. It must **not** include source-code content. Reference snippets by `path:line`, not by embedded code.
- `tool_calls[].args_digest` is a SHA-256 of the arguments, not the raw arguments. This protects entity strings the user may consider private.

## Output

Write `with-mcp.json` to the run directory and print a one-line summary: `<n> questions answered, <unknown> unknown, total <tokens> tokens, total <wall_ms>ms`. Return control to the orchestrator. Phase 4 (quality judgment) reads both `without-mcp.json` and `with-mcp.json` after this phase completes.
