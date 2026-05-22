# Phase 3 - With-MCP run

Answer every question from `questions.json` using **only archigraph MCP tools**. Record full per-question metrics. Your output is `with-mcp.json` in the run directory.

> **Context isolation:** This phase runs in a FRESH subagent context. You have NOT seen the grep-only results from Phase 2. Do not open `without-mcp.json` during this phase. This ordering is intentional: grep-only runs first (Phase 2) so that run cannot be contaminated by MCP results; you run second and must not look back.

## Allowed tools

You may call any archigraph MCP tool: `archigraph_whoami`, `archigraph_search`, `archigraph_describe`, `archigraph_related`, `archigraph_trace`, `archigraph_list_clusters`, `archigraph_get_source`, `archigraph_recent_activity`, `archigraph_list_link_candidates`, `archigraph_list_enrichment_candidates`, `archigraph_graph_stats`, `archigraph_patterns`, `archigraph_get_telemetry`, etc.

You may **not** call `rg`, `grep`, `Read` on source files, or any other non-MCP file-inspection tool. If a question is unanswerable through MCP alone, record `confidence: 0.0` and mark `unknown: true` - that is a valid result.

## Per-question protocol

For each question in `questions.json`:

1. Note the host's `usage_info` snapshot at question start (input/output/cache tokens emitted so far in this session).
2. Note `wall_clock_start` (monotonic time, RFC3339 nanos).
3. Answer the question using archigraph MCP tools. Take as many tool calls as needed but stop when you reach a defensible answer.
4. Note `wall_clock_end`.
5. Note the host's `usage_info` at question end.
6. Compute the delta and record the metrics below.

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
        {"tool": "archigraph_search", "args_digest": "sha256:...", "ok": true, "ms": 142}
      ],
      "tool_call_count": 4,
      "tools_used": ["archigraph_search", "archigraph_describe", "archigraph_related"],
      "metrics": {
        "input_tokens": 12345,
        "output_tokens": 678,
        "cache_read_tokens": 5000,
        "cache_creation_tokens": 0,
        "wall_clock_ms": 8421
      },
      "notes": "Mentioned archigraph_search returned 0 hits initially; widened query."
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
