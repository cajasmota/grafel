# Phase 2 - Without-MCP (grep-only) run

Answer every question from `questions.json` using **only** `rg` / `ripgrep` / `grep` / `Read` / `Bash` (no grafel MCP). Use the best non-MCP approach for each question — give grep+read a fair shot. Your output is `without-mcp.json`.

> **Context isolation:** This phase runs in a FRESH subagent context. You have NOT seen any MCP results. Do not open `with-mcp.json` — it does not exist yet (Phase 3 runs after you). This ordering is intentional: grep-only runs first so it cannot be contaminated by MCP results.

## Forbidden tools

- Any `grafel_*` MCP tool.
- Any reference to `with-mcp.json` (that file does not exist yet — Phase 3 runs after you).
- Any external code-search service.

If you accidentally see grafel state on disk (e.g., `.grafel/graph.json`), do not read it — it would bias the grep-only baseline.

## Allowed approach per category

These are reasonable best-effort strategies. Use whichever you would honestly use in a real grep-only session:

- **Entity lookup** - `rg -n 'class EntityName|struct EntityName|def EntityName'` then `Read` the definition file.
- **Reference finding** - `rg -n 'EntityName\.methodName\('` recursively, count results.
- **Cross-stack tracing** - `rg -n` for the entry-point keyword in the client repo, then `rg` the response shape in the server repo. Multi-pass.
- **Pattern discovery** - sample 3-5 instances of the kind via `rg`, read each, summarize the recurrence.
- **Architecture overview** - read top-level directory structure of each repo + each README + each `cmd/` or `apps/` subdir.
- **Subsystem deep-dive** - `rg` the subsystem keyword, follow imports, summarize.
- **Specific trace** - read the entry file, follow imports manually, multi-hop.
- **Data access** - `rg -n '\.fieldName'` then filter to read vs write contexts.
- **HTTP cross-repo** - `rg -n 'fetch\(|axios\.|http\.' client-repo`, then for each URL `rg -n` the matching route in server-repo.

## Per-question protocol

Same protocol as Phase 3 (with-MCP) but using grep tools instead of MCP:

1. Snapshot `usage_info` at question start.
2. Note `wall_clock_start`.
3. Answer using `rg` / `Read` / `Bash`. Keep tool calls bounded but fair - if a grep takes 3 passes, take the 3 passes.
4. Note `wall_clock_end`.
5. Snapshot `usage_info` at question end.
6. Record metrics.

## Output schema (`without-mcp.json`)

```json
{
  "version": 1,
  "method": "without-mcp",
  "iteration": 1,
  "started_at": "<RFC3339>",
  "ended_at": "<RFC3339>",
  "results": [
    {
      "id": "q01",
      "answer": "<the agent's prose answer>",
      "confidence": 0.7,
      "unknown": false,
      "tool_calls": [
        {"tool": "rg", "args_digest": "sha256:...", "ok": true, "ms": 88},
        {"tool": "Read", "args_digest": "sha256:...", "ok": true, "ms": 12}
      ],
      "tool_call_count": 6,
      "tools_used": ["rg", "Read"],
      "metrics": {
        "input_tokens": 8200,
        "output_tokens": 612,
        "cache_read_tokens": 4100,
        "cache_creation_tokens": 0,
        "wall_clock_ms": 5104
      },
      "notes": "Initial rg returned 200+ hits; narrowed pattern to method signature."
    }
  ]
}
```

## Fairness rules

- Use the best grep query you can think of, not a deliberately weak one. The benchmark is meaningless if the grep run is straw-manned.
- If the grep+read approach genuinely cannot answer a question (e.g., "what are the main subsystems" - hard without graph data), record `unknown: true` and a `notes` field explaining what stopped you. This is signal, not failure.
- Do not consult any cached or memorized prior knowledge about grafel or the codebase that would not be available to a grep-only agent.

## Token accounting

Use the host's `usage_info`; fall back to char/4 with `"estimated": true` if unavailable. Same accounting as Phase 3 (with-MCP).

## Privacy

- No source content in answer text; reference by `path:line`.
- `args_digest` not raw args.
- `args_digest` not raw args.

## Output

Write `without-mcp.json`. Print a one-line summary: `<n> questions answered, <unknown> unknown, total <tokens> tokens, total <wall_ms>ms`. Return to orchestrator.
