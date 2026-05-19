---
name: archigraph-quality-check
description: Pre-`/generate-docs` MCP quality benchmark. Runs a head-to-head between archigraph MCP and grep+read across ~10-15 representative questions on the registered group, judges quality against ground truth read from source, and emits a shareable markdown report with token / speed / quality tables, findings, and tuning recommendations. Use BEFORE generating documentation to confirm the MCP foundation is healthy.
when-to-use: User asks to "benchmark archigraph", "check MCP quality", "is archigraph helping", "validate MCP before docgen", or invokes `/archigraph-quality-check` explicitly. Run this BEFORE `/generate-docs` on a new group, after major archigraph changes, or to produce a comparison snapshot for tuning.
---

# archigraph-quality-check

Pre-docgen MCP quality benchmark. The skill answers a single question with data: **does archigraph MCP deliver value over grep+read on this group?** It produces a shareable markdown report the user passes back to the archigraph coordinator for tuning.

## When to use this skill

Invoke when the user asks for any of:

- "Benchmark archigraph on this group."
- "Is the MCP actually saving tokens here?"
- "Quality-check before we run /generate-docs."
- "Run a regression against the last benchmark."
- "/archigraph-quality-check" (slash command).

Do **not** invoke for one-off lookups, ad-hoc grep substitution, or to "test the daemon" - the daemon health-check is a separate concern.

## Why this skill exists

A predecessor MCP tool ("Tool A") was empirically found to consume **3-6× more tokens than grep+read** on representative questions, while not improving answer quality. This skill is the gate that confirms archigraph does not have the same failure mode. It is the formal step between "we built archigraph" and "we have evidence archigraph helps."

It runs **before** `/generate-docs` because docgen amplifies whatever bias the MCP has - if the MCP is slower and less accurate than grep on this group, docgen will burn tokens producing worse documentation than a grep-only pipeline would.

## Inputs the skill expects

- A resolved archigraph group (the skill calls `archigraph_whoami` first to confirm).
- A running archigraph daemon (per the user's `archigraph install` setup). **The skill never spawns a daemon.** If `archigraph_whoami` fails, stop and tell the user to start their daemon.
- Optional flags:
  - `--output <path>` - report destination. Default: `~/private/benchmarks/mcp-quality-bench-<YYYY-MM-DD>.md`.
  - `--iterations N` - run each question N times for noise reduction. Default 1. Reports median + stddev when N > 1.
  - `--focus <category>` - only generate questions from one of the nine categories (see Phase 1). Useful for stress-testing a known weak area.
  - `--question-set <path>` - JSON file with a user-curated question set instead of auto-generated. Schema documented in Phase 1.
  - `--baseline <path>` - prior report markdown to diff against. Surfaces deltas in token/quality/speed.

## Daemon discipline

This skill **runs against the user's existing daemon**. It does not start, restart, or touch the daemon. The user's `archigraph install` setup determines which daemon is in use.

- The agent never sets `ARCHIGRAPH_DAEMON_ROOT` or spawns `archigraph daemon`.
- The agent never kills any `archigraph` process.
- If MCP tool calls fail with "no daemon" errors, the skill stops and asks the user to run their own start command.

## Pass numbering (Phase 1 through Phase 5)

The skill is a strict pipeline. Each phase has a dedicated prompt under `prompts/`. A subagent reads the prompt and follows it; the orchestrator (this skill) tracks progress and gates each phase on the previous one's output.

| Phase | Prompt | Purpose |
|------|--------|---------|
| 1 | `prompts/01-question-generation.md` | Call `archigraph_whoami`, learn the group's real entities, generate ~10-15 questions across nine categories. Persist as `questions.json`. |
| 2 | `prompts/02-with-mcp-run.md` | Answer every question using archigraph MCP tools. Record tokens / time / tool calls / confidence per question. Persist as `with-mcp.json`. |
| 3 | `prompts/03-without-mcp-run.md` | Answer the **same** questions using only `rg` / `grep` / `Read` / `Bash`. No archigraph MCP. Same metrics. Persist as `without-mcp.json`. |
| 4 | `prompts/04-quality-judgment.md` | Determine ground truth by reading source code directly. Judge both runs against ground truth: full / partial / wrong / unknown plus per-question misses. Persist as `judgment.json`. |
| 5 | `prompts/05-report.md` | Render the markdown report at `--output`. Tables, findings, issues, recommendations, raw-data appendix. |

Each phase reads its predecessor's output from the run directory:

```
~/.archigraph/quality-check/<YYYY-MM-DD-HHMMSS>/
  questions.json       # Phase 1
  with-mcp.json        # Phase 2
  without-mcp.json     # Phase 3
  judgment.json        # Phase 4
  report.md            # Phase 5 (also copied to --output)
```

## Question categories

Phase 1 generates questions across nine categories. Each question is adapted to the registered group's actual entities (e.g., "what is UserService?" only if a `UserService`-like entity exists; otherwise substitute a real entity discovered via `archigraph_search`).

1. **Entity lookup** - "what is `<ClassName>`?"
2. **Reference finding** - "what calls `<Class>.<method>`?"
3. **Cross-stack tracing** - "how does the frontend `<feature>` flow reach the backend?"
4. **Pattern discovery** - "what's the convention for adding a new `<thing>`?"
5. **Architecture overview** - "what are the main subsystems in this group?"
6. **Subsystem deep-dive** - "describe the `<subsystem>`."
7. **Specific traces** - "trace from `<ui-entity>` to the DB row written."
8. **Data access** - "where does `<Entity>.<field>` get read or written?"
9. **HTTP cross-repo** - "what endpoints does the `<client-repo>` app actually call?"

## Token tracking

The host (Claude Code or compatible) provides `usage_info` per message: `{ input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens }`. The skill's per-question records must capture these directly from the host's reported usage for each agent message inside that question's scope, not estimate them.

- A question's `input_tokens` = sum of `input_tokens + cache_creation_input_tokens` across all agent messages emitted while answering that question.
- A question's `output_tokens` = sum of `output_tokens`.
- A question's `cache_read_tokens` = sum of `cache_read_input_tokens` (reported separately - cached reads are effectively free, so the report shows total-with-cache and total-without-cache).
- If the host does not surface usage info, the skill **falls back** to char-count / 4 as a rough estimate and clearly labels the report as "estimated tokens, host did not provide usage info".

## Ground truth methodology

Phase 4 reads source files **directly** to determine ground truth, independent of either Phase 2 or Phase 3. The judge does not see the with-MCP or without-MCP answer until ground truth is committed. Specifically:

1. Read the question.
2. Identify the files involved using a fresh `rg`/`Read` pass (no MCP) - the judge uses grep too, to avoid favoring either side.
3. Write the canonical answer.
4. **Then** open `with-mcp.json` and `without-mcp.json` and score each answer:
   - **full** - mentions every expected fact, no fabrications.
   - **partial** - mentions some expected facts, no fabrications.
   - **wrong** - contradicts the ground truth, or fabricates entities that don't exist.
   - **unknown** - the agent said "I don't know" or refused.
5. Record `misses` (expected facts not mentioned) and `extras` (fabrications or off-topic claims) per side.

This methodology is honest about MCP losses. If MCP confidently returned a wrong answer, the judge marks it wrong even if it sounded authoritative.

## Privacy

The skill **never logs file content** in the report or in any intermediate JSON. It logs:

- Entity kinds and counts.
- File **paths** (relative to repo root).
- Line numbers and span lengths.
- Tool names and call counts.

Source snippets are referenced by path+line, not embedded. The raw-data appendix in the report includes the agent's **answer text** (which the user wrote, and which the user is sharing voluntarily), but not source content.

## Beyond the minimum (flags)

- `--iterations N` - re-runs Phases 2-4 N times per question, reports median + stddev.
- `--focus <category>` - restricts Phase 1 to one category from the nine above. Useful when the user wants to stress-test, e.g., pattern discovery.
- `--question-set <path>` - skip Phase 1's generation, load the user's curated questions.
- `--baseline <path>` - load a prior report, surface per-question deltas: token saved or lost, quality changes, new failure modes.
- Cost projection - the report always extrapolates: "at this token rate, a 1000-query session would cost X with-MCP vs Y without-MCP."

## Acceptance criteria

- The skill's five prompt files exist and reference each other in the order documented above.
- The skill calls `archigraph_whoami` before generating any question, and questions reference only entities actually present in the group.
- Token counts come from the host's `usage_info`, with a labeled estimation fallback.
- Ground truth is established by an independent grep+read pass before scoring either answer.
- The report is written to `--output` (default `~/private/benchmarks/mcp-quality-bench-<date>.md`).
- The skill never spawns a daemon and never names real competitor tools in any artifact.

## Outputs

- `~/.archigraph/quality-check/<timestamp>/questions.json` - input set.
- `~/.archigraph/quality-check/<timestamp>/with-mcp.json` - Phase 2 results.
- `~/.archigraph/quality-check/<timestamp>/without-mcp.json` - Phase 3 results.
- `~/.archigraph/quality-check/<timestamp>/judgment.json` - Phase 4 scoring.
- `<--output>` (default `~/private/benchmarks/mcp-quality-bench-<date>.md`) - final shareable report.

## Related

- `/generate-docs` - the docgen skill this benchmark gates.
- ADR-0018 - pattern-discovery design; pattern questions in the test set verify it.
- ADR-0015 - repair passes; repair questions verify Pass 1a/1b/3a.
