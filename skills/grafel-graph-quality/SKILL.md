---
name: grafel-graph-quality
description: Continuous health check of the grafel knowledge graph. Runs a head-to-head benchmark between grafel MCP and grep+read across ~10-15 representative questions, judges quality against ground truth, and emits a shareable markdown report. Supports --since <sha> delta mode for CI gates and incremental re-runs.
when-to-use: User asks to "benchmark grafel", "check MCP quality", "is grafel helping", "validate MCP before docgen", or invokes /grafel-graph-quality explicitly. Also run as a CI gate with --since <sha> to confirm no regression since the last commit.
---

# grafel-graph-quality

Pre-docgen MCP quality benchmark. The skill answers a single question with data: **does grafel MCP deliver value over grep+read on this group?** It produces a shareable markdown report the user passes back to the grafel coordinator for tuning.

## When to use this skill

Invoke when the user asks for any of:

- "Benchmark grafel on this group."
- "Is the MCP actually saving tokens here?"
- "Quality-check before we run /generate-docs."
- "Run a regression against the last benchmark."
- "/grafel-quality-check" (slash command).
- "Run as a CI gate after indexing — /grafel-graph-quality --since <sha> -- benchmarks only entities changed since that commit."

Do **not** invoke for one-off lookups, ad-hoc grep substitution, or to "test the daemon" - the daemon health-check is a separate concern.

## MCP + grep, paired

grafel MCP gives you a navigable, accurate map of the code; grep gives you raw pattern matches.
Use MCP for structural questions: who calls X? what is the flow? where does Y live in the graph?
Use grep for raw enumeration: every `if err != nil`, every import line, every TODO.
Pair them: MCP narrows the search space; grep verifies edge-property questions MCP can't answer yet.

This benchmark exists precisely to measure whether grafel MCP earns its place in that pairing:
if it does not outperform grep+read on structural questions, the pair is unbalanced and docgen will
burn tokens for no quality gain. Phases 2 and 3 operationalise the pairing by running each side
independently then comparing outcomes.

## Why this skill exists

A predecessor MCP tool ("Tool A") was empirically found to consume **3-6× more tokens than grep+read** on representative questions, while not improving answer quality. This skill is the gate that confirms grafel does not have the same failure mode. It is the formal step between "we built grafel" and "we have evidence grafel helps."

It runs **before** `/generate-docs` because docgen amplifies whatever bias the MCP has - if the MCP is slower and less accurate than grep on this group, docgen will burn tokens producing worse documentation than a grep-only pipeline would.

## Inputs the skill expects

- A resolved grafel group (the skill calls `grafel_orient (view=me)` first to confirm).
- A running grafel daemon (per the user's `grafel install` setup). **The skill never spawns a daemon.** If `grafel_orient (view=me)` fails, stop and tell the user to start their daemon.
- Optional flags:
  - `--output <path>` - report destination. Default: `~/private/benchmarks/mcp-quality-bench-<YYYY-MM-DD>.md`.
  - `--iterations N` - run each question N times for noise reduction. Default 1. Reports median + stddev when N > 1.
  - `--focus <category>` - only generate questions from one of the nine categories (see Phase 1). Useful for stress-testing a known weak area.
  - `--question-set <path>` - JSON file with a user-curated question set instead of auto-generated. Schema documented in Phase 1.
  - `--baseline <path>` - prior report markdown to diff against. Surfaces deltas in token/quality/speed.
  - `--no-calibration` - skip Phase 6 (the extraction over/under audit). By default calibration always runs.
  - `--since <sha>` — delta mode: restrict Phase 1 question generation and Phase 6 calibration to entities whose source files changed since `<sha>`. Reads `grafel_index_status(since=<sha>)` to get the entity set. Full benchmark phases 2/3/4/5 still run on the restricted question set. See `prompts/07-delta-mode.md` for the complete delta procedure.

## Daemon discipline

This skill **runs against the user's existing daemon**. It does not start, restart, or touch the daemon. The user's `grafel install` setup determines which daemon is in use.

- The agent never sets `GRAFEL_DAEMON_ROOT` or spawns `grafel daemon`.
- The agent never kills any `grafel` process.
- If MCP tool calls fail with "no daemon" errors, the skill stops and asks the user to run their own start command.

## Pass numbering (Phase 1 through Phase 6)

The skill is a strict pipeline. Each phase has a dedicated prompt under `prompts/`. A subagent reads the prompt and follows it; the orchestrator (this skill) tracks progress and gates each phase on the previous one's output.

**Critical ordering: grep-only runs FIRST (Phase 2), MCP runs SECOND (Phase 3).** This prevents cross-run contamination — the grep agent answers before it can see which files or entities MCP found. Each of Phases 2 and 3 must run in a **separate, fresh subagent context**; the orchestrator spawns independent agents so neither phase sees the other's transcript.

| Phase | Prompt | Purpose |
|------|--------|---------|
| 1 | `prompts/01-question-generation.md` | Call `grafel_orient (view=me)`, learn the group's real entities, generate ~10-15 questions across nine categories. Persist as `questions.json`. |
| 2 | `prompts/02-without-mcp-run.md` | Answer every question using **only** `rg` / `grep` / `Read` / `Bash`. No grafel MCP. Record tokens / time / tool calls / confidence per question. Persist as `without-mcp.json`. **Runs FIRST** — fresh context, no MCP results in scope. |
| 3 | `prompts/03-with-mcp-run.md` | Answer the **same** questions using grafel MCP tools only. Same metrics. Persist as `with-mcp.json`. **Runs SECOND** — fresh context that has not seen Phase 2's transcript. See `prompts/03a-rpc-capture.md` for the daemon RPC capture protocol. |
| 3a | `prompts/03a-rpc-capture.md` | Daemon RPC capture protocol: log-offset snapshot procedure, CLI helper invocation, field semantics, error handling. Referenced by Phase 3 per-question metric collection. |
| 4 | `prompts/04-quality-judgment.md` | Determine ground truth by reading source code directly (independent grep+read pass, no MCP). Judge both runs against ground truth: full / partial / wrong / unknown plus per-question misses. Persist as `judgment.json`. |
| 5 | `prompts/05-report.md` | Render the markdown report at `--output`. Tables, findings, issues, recommendations, raw-data appendix. |
| 6 | `prompts/06-extraction-calibration.md` | Evaluate whether grafel is OVER- or UNDER-extracting on this group. Quantify phantom/duplicate nodes, noise, and missing relationships against grep+read ground truth. Persist as `calibration.json` and append an "Extraction calibration" section to the report. |
| 7 | `prompts/07-delta-mode.md` | Delta mode (--since): restrict question generation and calibration to entities changed since a given commit SHA. Skip this phase when --since is not provided. |

Phase 6 runs after Phase 5 (it consumes the same group + ground-truth passes and appends to the report). It is **not** gated on the benchmark questions — it is a structural audit of the graph itself, independent of the head-to-head. Run it whenever the benchmark runs; skip only if the user passes `--no-calibration`.

### Subagent isolation requirement

The orchestrator **must** enforce the following context boundaries:

- **Phase 2 subagent** — spawned with only `questions.json` in scope. Writes `without-mcp.json` and terminates. Must not have access to any MCP tool results.
- **Phase 3 subagent** — spawned after Phase 2 completes, also with only `questions.json` in scope. Must not receive Phase 2's transcript, tool-call log, or any side-channel that reveals which files or entities the grep run visited. Writes `with-mcp.json` and terminates.
- **Phase 4 subagent (judge)** — independent context. Establishes ground truth via its own fresh grep+read pass **before** opening `with-mcp.json` or `without-mcp.json`, so scoring is not anchored by either prior run.

If the orchestrator cannot guarantee full context isolation, it must at minimum instruct the Phase 3 agent explicitly: "Do not read `without-mcp.json`. Answer every question from scratch using only MCP tools."

Each phase reads its predecessor's output from the run directory:

```
~/.grafel/quality-check/<YYYY-MM-DD-HHMMSS>/
  questions.json       # Phase 1
  without-mcp.json     # Phase 2 (grep-only, runs FIRST)
  with-mcp.json        # Phase 3 (MCP, runs SECOND)
  judgment.json        # Phase 4
  report.md            # Phase 5 (also copied to --output)
  calibration.json     # Phase 6 (extraction over/under audit; report section appended)
```

## Question categories

Phase 1 generates questions across nine categories. Each question is adapted to the registered group's actual entities (e.g., "what is UserService?" only if a `UserService`-like entity exists; otherwise substitute a real entity discovered via `grafel_find`).

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

## Extraction calibration (Phase 6)

The benchmark answers "does the MCP beat grep on real questions?" Phase 6 answers a different, complementary question: **is the graph itself the right size?** A graph can win the head-to-head while still being miscalibrated — too many junk nodes (over-extraction) or too few real edges (under-extraction). Both degrade docgen and erode trust in counts. Phase 6 quantifies each direction against grep+read ground truth and emits an "Extraction calibration" table.

### Over-extraction (noise / false positives)

Things in the graph that should NOT be there, or that inflate counts:

- **Duplicate-kind nodes** - one source symbol emitted as multiple kind-tagged nodes (e.g. a Django `ViewSet` appearing as both a `Component` and a `View`; a Celery task appearing as `ScheduledJob` + `Task` + `Operation`). Detect by grouping search results on `(name, source_file)` and counting distinct `entity_id`/`kind`. Report the duplication factor (nodes per real symbol).
- **Statement-level noise** - expressions, assignments, or f-strings extracted as standalone entities (e.g. `error_message = f"..."` as kind `Operation`). Detect by sampling entity names that contain `=`, `f"`, `return`, operators, or are not valid identifiers.
- **Framework scaffolding** - auto-generated routes/handlers that are not hand-written app surface (e.g. Django admin `/admin/...` CRUD endpoints, migration files). Count them as a share of the relevant kind and flag for optional pruning.
- **Generated / vendored pollution** - nodes from `node_modules`, `dist/`, `build/`, `migrations/`, `*.generated.*`, protobuf/openapi output. Detect by `source_file` path patterns.
- **Phantom edges** - relationships that grep proves do not exist (rare; sample-verify a handful of high-degree edges).
- **Trivial entities** - getters/setters/`__init__`-only stubs counted as first-class operations when they carry no graph value.

Quantify each as a count and a rate (share of the affected kind or of total entities). Give 1-3 concrete examples per category with `path:line`.

### Under-extraction (gaps / false negatives)

Things that SHOULD be in the graph but are missing:

- **Missing relationships that should exist** - e.g. test entities present but **zero** `TESTS` edges; Celery/pub-sub topics with null publishers or subscribers; cross-repo HTTP calls left as orphans because the client path (`/inspections/{id}`) doesn't match the server route (`/api/v1/inspections/{pk}`) — a prefix/param-name normalization gap, not a real missing endpoint. Quantify orphan-call rate and cross-repo link coverage (linked ÷ linkable).
- **Missing entities** - real symbols grep finds that `grafel_find` / `grafel_inspect` cannot. Sample a few known classes/functions from each repo and confirm presence.
- **Empty qualified_names** - entities whose `qualified_name` is `""`. These break path-finding and cross-repo joins. Report the rate by kind.
- **Unlinked framework patterns** - DI bindings, signal/event handlers, route→handler wiring that exist in source but produce no edge.
- **Missing kinds** - structurally important kinds that are absent or near-empty (e.g. a `Process` label expected but unpopulated).

For each gap, establish ground truth with grep/read on the real repos before claiming a miss, so the audit doesn't blame the graph for something that genuinely isn't in the code.

### Output: the calibration table

```markdown
## Extraction calibration

| Direction | Issue | Count | Rate | Example (path:line) |
|---|---|---:|---:|---|
| Over | Duplicate-kind nodes (ViewSet=Component+View) | ... | ...× per symbol | ... |
| Over | Statement-level noise (f-strings as Operation) | ... | ...% of Operation | ... |
| Over | Django admin scaffolding endpoints | ... | ...% of endpoints | ... |
| Under | Test entities with 0 TESTS edges | ... | ...% | ... |
| Under | Orphan cross-repo HTTP calls | ... | ...% of calls | ... |
| Under | Empty qualified_names | ... | ...% | ... |

**Calibration verdict:** `<over-biased / under-biased / balanced>` — one-line justification.

### Prune recommendations (over-extraction)
- ...

### Add recommendations (under-extraction)
- ...
```

The verdict and recommendations feed the grafel coordinator directly: "prune X" and "wire up Y" are actionable indexer changes. Recommendations must cite the count/rate that motivates them.

## Privacy

The skill **never logs file content** in the report or in any intermediate JSON. It logs:

- Entity kinds and counts.
- File **paths** (relative to repo root).
- Line numbers and span lengths.
- Tool names and call counts.

Source snippets are referenced by path+line, not embedded. The raw-data appendix in the report includes the agent's **answer text** (which the user wrote, and which the user is sharing voluntarily), but not source content.

## Beyond the minimum (flags)

- `--iterations N` - re-runs Phases 2-3-4 N times per question (each pair in fresh isolated contexts), reports median + stddev.
- `--focus <category>` - restricts Phase 1 to one category from the nine above. Useful when the user wants to stress-test, e.g., pattern discovery.
- `--question-set <path>` - skip Phase 1's generation, load the user's curated questions.
- `--baseline <path>` - load a prior report, surface per-question deltas: token saved or lost, quality changes, new failure modes.
- Cost projection - the report always extrapolates: "at this token rate, a 1000-query session would cost X with-MCP vs Y without-MCP."

## Acceptance criteria

- The skill's six prompt files exist: `01` through `06`, with `02-without-mcp-run.md` (grep first) preceding `03-with-mcp-run.md` (MCP second).
- The orchestrator spawns Phase 2 and Phase 3 as independent subagents with no shared transcript.
- The skill calls `grafel_orient (view=me)` before generating any question, and questions reference only entities actually present in the group.
- Token counts come from the host's `usage_info`, with a labeled estimation fallback.
- Phase 3 captures `[mcp-rpc] tool=<X> elapsed=<N>ms` lines from `~/.grafel/logs/daemon.log` during each question by running `grafel bench-capture rpc --start $START --end $END` (#2298) and merges its JSON into the question's `metrics` block. The `mcp_rpc_*` field definitions are the authoritative schema in `schema/with-mcp-artifact.schema.json` (#2302). The Phase 5 report includes a "Handler vs Transport" subsection splitting wall time into handler and transport (#2291).
- Ground truth is established by an independent grep+read pass in Phase 4 before scoring either answer — Phase 4 does not read `without-mcp.json` or `with-mcp.json` until after its own grep pass is committed.
- The report is written to `--output` (default `~/private/benchmarks/mcp-quality-bench-<date>.md`).
- The skill never spawns a daemon and never names real competitor tools in any artifact.
- Phase 6 runs by default (unless `--no-calibration`), writes `calibration.json`, and appends an "Extraction calibration" table with quantified over- and under-extraction rows plus prune/add recommendations, each citing a count or rate established against grep+read ground truth.

## Outputs

- `~/.grafel/quality-check/<timestamp>/questions.json` - input set (Phase 1).
- `~/.grafel/quality-check/<timestamp>/without-mcp.json` - Phase 2 results (grep-only, runs first).
- `~/.grafel/quality-check/<timestamp>/with-mcp.json` - Phase 3 results (MCP, runs second). **Schema:** `schema/with-mcp-artifact.schema.json` (#2302). The `metrics.mcp_rpc_*` fields are populated by `grafel bench-capture rpc` (#2298).
- `~/.grafel/quality-check/<timestamp>/judgment.json` - Phase 4 scoring.
- `~/.grafel/quality-check/<timestamp>/calibration.json` - Phase 6 over/under-extraction audit.
- `<--output>` (default `~/private/benchmarks/mcp-quality-bench-<date>.md`) - final shareable report (includes the "Extraction calibration" section).

## Related

- `/generate-docs` - the docgen skill this benchmark gates.
- ADR-0018 - pattern-discovery design; pattern questions in the test set verify it.
- ADR-0015 - repair passes; repair questions verify Pass 1a/1b/3a.

## Read next

After confirming graph quality, enrich dashboard panels (Paths / Flows / Topology) with structured metadata:
→ `/grafel-graph-enrich` — emit YAML frontmatter for endpoints, flows, and topics so the dashboard lights up.

Or proceed directly to documentation:
→ `/grafel-tech-docs` — generate per-module technical documentation for engineers.
→ `/grafel-business-docs` — generate PM-facing capabilities, journeys, and business rules.
