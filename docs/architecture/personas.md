# Persona Architecture for archigraph

**Status:** Canonical architectural contract — v3 (interactive hire-on-demand)
**Scope:** Persona definition, shape, delivery, orchestration, catalog, communication styles, escalation, anti-patterns, phasing.
**Supersedes:** v1/v2 ("auto-fan-out + editor synthesis") shipped in PR #2449.

---

## 1. What is a persona?

A persona is an **agent definition file** — a markdown document that instructs a user's coding agent (Claude Code, Windsurf, Cursor) to adopt a specialist role, navigate a codebase's archigraph knowledge graph + generated documentation, and **converse with the user from that lens**. Personas are **not CLI commands**, **not daemons**, **not web-UI features**, and **not auto-firing report generators**.

### 1.1 Paradigm: hire-on-demand interactive consultant

A persona is a **consultant the user hires**. Hiring works like this:

1. The user invokes the `archigraph-consult` skill (or types `/archigraph-consult`).
2. The skill presents the catalog of available consultants.
3. The user picks one (or asks the skill to recommend, given a problem).
4. The chosen persona becomes the **active consultant** for the conversation.
5. The active consultant answers the user's questions, explores the codebase via archigraph MCP, and delivers analysis in whatever shape the question demands — prose, ASCII diagram, table, code sample, analogy, severity matrix.
6. The user may release the consultant, switch consultants, or ask the active consultant to **Consult-Out** to a peer.

There is **no auto-fan-out**, no automatic multi-persona run, no editor synthesis pass, and no implicit findings-graph materialisation. Those were the v1/v2 model and have been retired.

### 1.2 What changed from PR #2449

| Aspect | v1/v2 (PR #2449) | v3 (this doc) |
|---|---|---|
| Trigger | Auto after `/generate-docs` | Explicit user invocation |
| Mode | Batch fan-out across all personas | One active consultant at a time |
| Output | Mandatory markdown reports + JSON findings file | Whatever shape best answers the user's question |
| Findings → graph | Auto-saved at confidence ≥ 0.7 | Saved only when user asks |
| Cross-persona | Editor synthesis pass post-run | Consult-Out: active persona pulls in a peer mid-conversation |
| Persona body | Fixed 5-section template ending in OUTPUT shape | Role + READ + ANALYSIS lens + communication styles + Consult-Out triggers |
| Stop criteria | Hard caps (15 findings, all questions answered) | User releases or switches |

---

## 2. Canonical persona shape

### 2.1 Frontmatter

```yaml
---
name: archigraph-<persona-name>           # lowercase, hyphens, prefixed archigraph-
description: >
  One tight sentence: what this consultant is good at, and what kind of
  user question signals "hire this one".
# Recommended model: <tier> — one-line rationale. Host agent may override.
model: sonnet   # or opus — see Section 2.3 for per-persona recommendations
---
```

**Tool inheritance:** Personas omit the `tools:` field from frontmatter to inherit the host agent's full toolset (Read, Write, Bash, all user-configured MCPs, etc.). Safety is enforced by the host agent's permission model, not by per-persona allowlists. If a persona has a specific tool restriction by design, document it in the Role section rather than via frontmatter.

### 2.3 Per-persona model recommendations

The `model:` frontmatter field is an **opinionated suggestion** to the host agent's invocation layer. It is not enforced — the host agent picks the active model and may override it based on user settings, cost policy, or context window. When omitted, the host agent decides.

| Persona | Recommended model | Rationale |
|---|---|---|
| `architect` | `opus` | Multi-hop structural inference across large dependency graphs requires depth |
| `security-auditor` | `opus` | Subtle vulnerability detection needs deep reachability and adversarial reasoning |
| `performance-reviewer` | `opus` | Multi-pass hot-path analysis holds large call-graph contexts simultaneously |
| `business-analyst` | `sonnet` | Business synthesis from route/flow data does not require deep technical inference |
| `refactor-critic` | `sonnet` | Refactor signals are clear from graph degree/duplication data |
| `api-designer` | `sonnet` | API review is primarily inventory and spec comparison work |
| `data-engineer` | `sonnet` | Schema and query analysis follows clear structural patterns |
| `qa-reviewer` | `sonnet` | Test inventory and TESTS-edge coverage analysis is structured enumeration |
| `solutions-architect` | `opus` | Cross-service architectural reasoning requires multi-hop inference across repo boundaries |
| `devops-reviewer` | `sonnet` | Config review follows structured enumeration; deep inference not required |
| `compliance-officer` | `opus` | High false-positive risk requires careful multi-hop reasoning to avoid erroneous findings |
| `dx-engineer` | `sonnet` | DX signals follow structured test-edge and import-graph enumeration |

**Override contract:** The host agent MUST honour an explicit `--model` flag from the user (e.g. `/archigraph-consult --model haiku`) over the persona's own `model:` recommendation. The recommendation is a default, not a lock.

### 2.2 Body structure (v3)

Every persona body follows this template:

```markdown
## Role
One paragraph: who you are, what lens you bring, what you refuse to speculate on without graph evidence. You are *interactive* — you respond to the user's questions, you do not auto-emit a report.

## READ instructions
Ordered list of graph queries the persona runs at the start of a conversation
to ground itself. Light-weight on hire; deeper queries are issued on demand
as the user's questions require.

## ANALYSIS lens
The questions this persona habitually asks of a codebase. Not a checklist
to mechanically complete — a lens through which user questions are
interpreted.

## Communication styles for this domain
The persona's toolkit for explaining things: which of ASCII diagrams,
tables, analogies, code samples, severity matrices, sequence diagrams the
persona reaches for, and when. Each persona's list is domain-tuned.

## When to ask for an expert (Consult-Out)
Named peer personas this consultant typically hands off to, plus the
trigger conditions. See Section 5.

## Response shape
The persona responds in whatever shape best serves the user's question.
No fixed report template. If the user asks "is this module too coupled?",
answer that — don't deliver a 7-section structural audit.

## When the user asks to save this analysis
Documents how to persist findings on explicit user request. Default path:
`~/.archigraph/groups/<group>/findings/<persona>-<short-slug>-<YYYY-MM-DD>.md`.
Confirm path with user if ambiguous. Also offers `archigraph_save_finding`
as the canonical graph-persistence path when the MCP exposes it.
```

There is **no STOP-criteria section** in v3. The session ends when the user releases the persona.

There is **no OUTPUT format section** in v3. Personas respond to questions in domain-appropriate shapes.

### 2.4 Save-finding affordance contract

Findings save **only on explicit user request**. The trigger phrases are: "save this", "write a report", "create a follow-up doc", or equivalent. On trigger:

1. The persona uses the host agent's `Write` tool to save a markdown file at the default path (`~/.archigraph/groups/<group>/findings/<persona>-<short-slug>-<YYYY-MM-DD>.md`).
2. If the path is ambiguous (e.g. multiple groups, or the user specifies a different location), the persona confirms the path with the user before writing.
3. If `archigraph_save_finding` is available in the host MCP, the persona SHOULD also call it — this is the canonical path for graph-registered findings that appear in dashboard panels. The `Write` call and the MCP call are not mutually exclusive.
4. The persona does **not** auto-save at confidence thresholds. There is no background materialisation. This was the v1/v2 model and is retired.

---

## 3. Cross-platform delivery

Personas must work across coding-agent hosts, but only Claude Code provides true context isolation. The "hire" semantics are emulated differently on each platform.

### 3.1 Delivery matrix

| Platform | Hire mechanism | Active-state tracking | Isolation | Status |
|---|---|---|---|---|
| **Claude Code** | Subagent at `.claude/agents/archigraph-<name>.md` — invoked via Task tool with subagent_type | Per-subagent context (native) | Yes | **Working** |
| **Windsurf (Cascade)** | Workflow at `.windsurf/workflows/archigraph-consult.md` prompt-injects the persona body into the shared Cascade context | Conversation-level marker ("ACTIVE PERSONA: <name>") that the workflow sets; main Cascade reads it on every turn | No (shared context) | **Working with caveats** — see 3.4 |
| **Cursor** | Slash command at `.cursor/commands/archigraph-consult.md` + rules under `.cursor/rules/archigraph-personas.mdc`; Agents Window can run a hire in a side tab for isolation | Active persona named in command frontmatter; Agents Window provides per-tab isolation | Partial (per-tab) | **Working** |
| **Codex / others** | Markdown shim referencing the persona body | None — manual | None | **Deferred** |

### 3.2 Canonical source-of-truth

The persona bodies live at `skills/archigraph-consult/personas/<name>.md` (this repo). All platform wrappers **reference** these bodies — they do not duplicate the persona content. The wrappers are thin: catalog enumeration, hire mechanic, Consult-Out plumbing.

### 3.3 Claude Code path (canonical)

The `archigraph-consult` skill:

1. Lists the catalog (reads `personas/*.md` frontmatter `description:` fields).
2. Asks the user which to hire (or interprets a natural-language request).
3. Spawns a subagent with the persona body as the system prompt, **scoped to the user's conversation** — i.e. the subagent stays "alive" and the parent agent forwards subsequent user turns to it until the user releases.

In practice, the simplest implementation is: the parent (main Claude Code agent) loads the persona body **inline** into the current conversation and itself adopts the role. A true subagent is used only for Consult-Out (Section 5) when isolation is genuinely needed.

### 3.4 Windsurf path

Cascade has one shared context. "Hiring" works by:

1. The `archigraph-consult` workflow runs in the current Cascade context.
2. The workflow injects a system-level reminder: `ACTIVE PERSONA: archigraph-<name>. Body follows: <inlined persona body>.`
3. Cascade adopts the role for subsequent turns.
4. Releasing = the user says "release the consultant" or invokes the workflow again with a different persona.

**Caveat:** there is no enforcement. If the user changes topic mid-conversation Cascade may drift out of the persona. The workflow includes a self-check step the user can re-trigger ("reconfirm active persona"). True isolation requires a sidecar (deferred).

### 3.5 Cursor path

Cursor's Agents Window provides per-tab isolation: hiring a consultant opens a new agent tab with the persona body as system prompt. The slash command + rule provide the in-line fallback for users who prefer the chat panel.

---

## 4. Orchestration via `archigraph-consult`

The skill is the single entry point. Flow:

```
User: /archigraph-consult
  └─ archigraph-consult skill
       ├─ pre-flight: archigraph_whoami, tech-docs presence check
       ├─ enumerate catalog (read personas/*.md frontmatter)
       ├─ ask user: "which consultant would you like to hire?"
       │   (or interpret natural-language "I need an architecture review" → architect)
       ├─ activate selected persona (inline body load or subagent spawn)
       └─ conversation continues with active persona answering questions
              │
              └─ user may: ask questions / request Consult-Out / switch persona / release
```

The skill does not run all personas. It does not produce a synthesis. It does not auto-save findings. Those behaviours belonged to v1/v2.

---

## 5. Consult-Out — the escalation pattern

A consultant working on a problem may realise they need a peer's lens. Example: the security-auditor is tracing an auth flow and spots that one handler does a 200 ms sync DB scan per request — that's a performance concern. The security-auditor isn't qualified to opine on caching strategy. They Consult-Out to performance-reviewer.

### 5.1 Mechanic

The active consultant signals the need with a structured callout in their response. The callout now carries the full multi-hop envelope (see Section 5.4 for the schema):

```
> [CONSULT-OUT]
> target: archigraph-performance-reviewer
> reason: Latency optimisation is outside my (security) lens
> depth: 1
> chain: [security-auditor]
> context:
>   original_ask: "is this login flow safe?"
>   prior_findings:
>     - persona: security-auditor
>       summary: |
>         - auth.LoginHandler (entity_id: 4abf…) passes OWASP auth checks
>         - handler issues a synchronous DB scan per request (~200 ms)
>         - no injection vectors found in query construction
>
> Shall I bring them in?
```

The user replies yes/no. If yes, the orchestrator:

1. **Claude Code:** spawns the requested persona as a true subagent (Task tool with subagent_type), passing the carry-over context as the opening message. The original consultant remains active in the parent conversation. The peer's response is summarised back to the user with `[CONSULT-IN: performance-reviewer]` tagging.
2. **Windsurf:** appends a second `ACTIVE PERSONA` marker scoped to this turn only ("for this answer, also adopt archigraph-performance-reviewer's lens"). The shared context means both lenses inform the same response. After the answer, the marker expires.
3. **Cursor:** opens a new Agents-Window tab with the peer, passing carry-over context.

### 5.2 Carry-over context (required)

Every Consult-Out call MUST include:

- The entity_ids under discussion.
- The user's original question (preserved verbatim from the first hop — do not paraphrase).
- A 2–4 bullet summary of what the original consultant has found so far.
- The specific sub-question the peer is being asked to answer.
- The accumulated `prior_findings` from every prior hop (see Section 5.4).

This avoids the peer re-doing any previous hop's READ phase.

### 5.3 When NOT to Consult-Out

- The peer's lens overlaps trivially with the active consultant's — handle it inline.
- The user has not yet engaged deeply with the active consultant's answer.
- The current depth is already 3 (hard cap — see Section 5.5).
- The target persona already appears in `chain` (cycle — see Section 5.6).
- More than 2 Consult-Outs have already happened in this conversation (panel sprawl).

### 5.4 Multi-hop [CONSULT-OUT] schema

Starting with this release (issue #2473), the `[CONSULT-OUT]` block is a structured YAML-in-blockquote envelope. All fields are required when depth > 1; `depth` and `chain` are required at all depths for forward compatibility.

```yaml
# [CONSULT-OUT] envelope (YAML fields, blockquote-formatted in persona output)
target: archigraph-<persona-name>       # the peer being recruited
reason: <one-line justification>        # why this peer's lens is needed
depth: <integer, 1-indexed>             # current hop number (1 = first Consult-Out)
chain: [<persona-a>, <persona-b>, ...]  # personas already in the chain (NOT including target)
context:
  original_ask: "<verbatim user question from hop 0>"
  prior_findings:
    - persona: <name>                   # one entry per prior hop
      summary: |
        - <2-3 line takeaway from that hop>
```

**Rules:**

| Field | Rule |
|---|---|
| `depth` | Incremented by 1 at each hop. Personas receiving a Consult-Out at depth ≥ 3 MUST refuse to chain further. |
| `chain` | The invoking persona appends its own name before sending. The target checks that its own name is NOT in `chain`. |
| `prior_findings` | Each hop appends its own summary entry before handing off. The deepest expert receives all prior findings. |
| `original_ask` | Copied verbatim from the first hop's `context.original_ask`. Never overwritten or paraphrased by intermediate hops. |

### 5.5 Depth cap

The maximum chain depth is **3 hops** (depth values 1, 2, 3). A persona receiving a Consult-Out at `depth: 3` MUST answer the sub-question itself (within its best-effort lens) rather than chaining further. If the question is genuinely outside its lens, it answers with "evidence insufficient in my lens — consider releasing and switching consultants directly."

The depth cap may be raised by the user via an explicit instruction in the conversation (e.g. "allow up to 5 hops for this investigation"). The persona acknowledges the override and records the new cap in the envelope. Default cap: 3.

### 5.6 Cycle detection

Before emitting a `[CONSULT-OUT]` block, the persona MUST check whether the intended `target` already appears in `chain`. If it does:

- Do NOT emit the `[CONSULT-OUT]` block.
- Inform the user: "`archigraph-<target>` is already in the consultation chain (<chain>). Consulting them again would create a loop. Would you like me to answer within my own lens instead, or switch to a different expert?"

Cycles are prevented at the persona level; the orchestrator does not need a separate guard (defence-in-depth, not primary enforcement).

### 5.7 Worked example: 2-hop chain (architect → security-auditor → data-engineer)

**Scenario:** The user asks the `architect` persona: *"Is our persistence layer correctly isolated from the HTTP handlers?"*

**Hop 0 — architect is the active consultant**

Architect traces the call graph and finds `api.OrderHandler` calls `db.RawQueryRunner` directly, bypassing the service layer. This is a layering violation. While examining the raw queries, architect spots parameterisation patterns that look suspect — but injection risk assessment is outside the architect's lens.

Architect emits:

```
> [CONSULT-OUT]
> target: archigraph-security-auditor
> reason: Raw query patterns at the layering violation boundary may carry injection risk — security lens needed
> depth: 1
> chain: [architect]
> context:
>   original_ask: "Is our persistence layer correctly isolated from the HTTP handlers?"
>   prior_findings:
>     - persona: architect
>       summary: |
>         - api.OrderHandler (entity_id: 7c3a…) calls db.RawQueryRunner directly (CALLS edge, no service hop)
>         - Layering violation: presentation → persistence bypass confirmed
>         - 3 other handlers share this pattern (entity_ids: 8b1f…, 9d44…, 2e7c…)
>
> Shall I bring them in?
```

**Hop 1 — security-auditor is consulted**

Security-auditor receives the envelope (`depth: 1`, `chain: [architect]`). They inspect `db.RawQueryRunner` and confirm parameterised queries are used — no injection. But they spot that `db.RawQueryRunner` reads a raw schema column named `user_pii_raw` without any field-level encryption annotation. Data handling is outside the security-auditor's lens (that's schema design territory).

Security-auditor appends their findings and emits:

```
> [CONSULT-OUT]
> target: archigraph-data-engineer
> reason: Unencrypted PII column accessed via raw query — schema and migration hygiene lens needed
> depth: 2
> chain: [architect, security-auditor]
> context:
>   original_ask: "Is our persistence layer correctly isolated from the HTTP handlers?"
>   prior_findings:
>     - persona: architect
>       summary: |
>         - api.OrderHandler calls db.RawQueryRunner directly (layering violation)
>         - 3 additional handlers share the pattern
>     - persona: security-auditor
>       summary: |
>         - Parameterised queries confirmed — no SQL injection risk
>         - db.RawQueryRunner reads column `user_pii_raw` (entity_id: 1fa9…) without encryption annotation
>         - No auth check on the column access path
>
> Shall I bring them in?
```

**Hop 2 — data-engineer is consulted**

Data-engineer receives the envelope (`depth: 2`, `chain: [architect, security-auditor]`). They inspect `user_pii_raw`, find no migration that added encryption, and flag it as a schema hygiene issue. Depth cap is not yet reached, but data-engineer has no further peer to bring in for this sub-question — they answer directly.

Data-engineer returns `[CONSULT-IN: data-engineer]` to the conversation with:
- `user_pii_raw` has no encryption-at-rest annotation in any migration (entity search: 0 results).
- Recommend adding a column-level encryption migration and updating `db.RawQueryRunner` to decrypt in the service layer.

**Return path:** Each `[CONSULT-IN]` reply bubbles back up. The user sees three tagged responses in sequence. The original `architect` persona remains active for the parent conversation.

---

## 6. Communication styles catalog

Personas use rich communication. The catalog of styles:

| Style | Best for | Example trigger |
|---|---|---|
| **ASCII call graph** | Showing fan-in/fan-out, dependency chains, blast radius | "What depends on this function?" |
| **ASCII sequence diagram** | Multi-actor flows (HTTP request → service → DB → response) | "Walk me through a login" |
| **ASCII flow chart** | Branching logic, decision points, state transitions | "How does the order state machine work?" |
| **Comparison table** | Trade-offs between options, before/after, multiple modules side-by-side | "Should we use approach A or B?" |
| **Severity matrix** | Risk ranking across a set of findings | "What are the worst issues?" |
| **Decision matrix** | Choosing among options on multiple criteria | "Which DB should we pick?" |
| **Domain analogy** | Explaining technical concepts to non-technical stakeholders | "Why is this slow?" |
| **Concrete code sample** | Showing the fix, not just describing it | "How do I fix this N+1?" |
| **Severity / confidence callout** | Single high-impact finding with action | "Is this vulnerable?" |
| **Module-ownership table** | Mapping entities to modules to teams | "Who owns this code?" |

Each persona's body lists the subset of styles relevant to its domain (e.g. architect leans on ASCII call graphs + cluster tables; business-analyst leans on domain analogies + user-journey flow charts).

---

## 7. Persona catalog

Twelve personas ship. The catalog count must match across this doc, `SKILL.md`, and the filesystem at `skills/archigraph-consult/personas/`.

| # | Name | Lens | Primary graph queries | Status |
|---|---|---|---|---|
| 1 | `architect` | Module layering, coupling, cyclic deps, god modules, boundary violations | `archigraph_clusters`, `archigraph_expand` (IMPORTS/CALLS), `archigraph_stats` | Shipped |
| 2 | `security-auditor` | Auth gaps, PII exposure, injection risks, secrets, attack surface | `archigraph_traces` (auth entry points), `archigraph_expand`, `archigraph_find` | Shipped |
| 3 | `business-analyst` | Capability coverage, feature gaps, business rule completeness, user-journey gaps | `archigraph_traces`, `archigraph_find` (route entities), `archigraph_clusters` | Shipped |
| 4 | `performance-reviewer` | Hot paths, N+1 queries, sync blocking, unbounded queries, over-fetching | `archigraph_expand`, `archigraph_traces`, `archigraph_find` (DB call patterns) | Shipped |
| 5 | `refactor-critic` | Complexity hotspots, duplication, dead code, long call chains, tech-debt | `archigraph_stats`, `archigraph_expand` (zero-caller nodes), `archigraph_clusters` | Shipped |
| 6 | `api-designer` | Endpoint naming, REST/RPC convention consistency, versioning, OpenAPI gaps | `archigraph_find` (http_endpoint), `archigraph_inspect`, `archigraph_cross_links` | Shipped |
| 7 | `data-engineer` | Schema quality, migration hygiene, ORM patterns, missing indexes, FK integrity | `archigraph_find` (schema/model), `archigraph_expand`, `archigraph_traces` | Shipped |
| 8 | `qa-reviewer` | Test coverage by module, missing test types, untested critical paths | `archigraph_expand` (TESTS edges), `archigraph_find`, `archigraph_traces` | Shipped |
| 9 | `solutions-architect` | Cross-service boundaries, inter-repo contracts, coupling, blast-radius | `archigraph_cross_links`, `archigraph_expand`, `archigraph_traces` | Shipped (with limitations) — signal requires cross_links data populated; limited for single-repo groups |
| 10 | `devops-reviewer` | CI/CD config, GitHub Actions pinning, build hygiene, graph-visible infra config | `archigraph_status`, `archigraph_find`, `archigraph_subgraph` | Shipped (with limitations) — does NOT index Terraform/k8s; CI/YAML slice only |
| 11 | `compliance-officer` | PII field detection, audit-trail gaps, sensitive data flow surface scan | `archigraph_find` (field names), `archigraph_inspect`, `archigraph_expand` (READS_FIELD/WRITES_FIELD) | Shipped (with limitations) — name-match heuristics only; no data-classification layer; high false-positive rate |
| 12 | `dx-engineer` | Test desert modules, circular imports, god entry-points, module size outliers | `archigraph_clusters`, `archigraph_expand` (TESTS/IMPORTS), `archigraph_stats` | Shipped (with limitations) — test/import-graph signals only; no docs/README or build-time review |

---

## 8. What personas do NOT do (v3 anti-section)

This section is non-negotiable. Any implementation that violates these invariants is wrong.

- **No auto-report.** Personas do not emit a report after `/generate-docs` or any other skill. They speak only when hired.
- **No daemon spawn.** Personas do not run as background processes.
- **No MCP-tool-registry membership.** Personas consume MCP tools; they are not exposed as MCP endpoints.
- **No implicit fan-out.** The skill does not silently run all 8 personas. The user picks one (or asks the skill to recommend one).
- **No budget management in persona files.** Token/cost concerns live in the host, not the persona body.
- **No fixed OUTPUT shape.** Personas respond in whatever shape best answers the user's question. The five-section template from v1/v2 is retired.
- **No editor synthesis pass.** There is nothing to synthesise — there's one active consultant at a time. Cross-persona reasoning happens through Consult-Out, not post-hoc.
- **No web-UI surface.** Findings the user explicitly saves may render in the dashboard, but personas themselves are not dashboard items.
- **No install CLI.** Personas are markdown; install is a file copy.
- **No CLI invocation.** There is no `archigraph architect` command.

---

## 9. Composable skills

### 9.1 Pattern: shared skill as a building block

Personas share boilerplate steps (confirm the graph is loaded, orient via inspect, traverse via expand). Rather than duplicating these steps in every persona body, we extract them into a **composable shared skill** — a standalone `SKILL.md` that personas reference with a one-liner.

**Canonical example:** `skills/archigraph-graph-read/SKILL.md`

This skill documents the `status → inspect → expand` READ protocol that every persona uses as its grounding pass. Personas reference it as:

```markdown
## READ Protocol
Follow `archigraph-graph-read` (status → inspect → expand). Stop reading when the entities answer the question.
```

The persona's ANALYSIS questions (which are unique per lens) remain in the persona body. Only the boilerplate navigation steps live in the shared skill.

### 9.2 Why this matters

- **Single source of truth** for the READ protocol: changes to `archigraph-graph-read/SKILL.md` propagate to all 8 personas without hunting through persona files.
- **Smaller persona files**: each persona body focuses on its unique analytical lens rather than repeating orientation steps.
- **Composability signal**: future shared skills (`archigraph-graph-write`?, `archigraph-graph-search`?) follow the same `skills/<name>/SKILL.md` pattern. A skill is composable if it can be referenced from multiple persona bodies as a one-liner.

### 9.3 Convention for future composable skills

| Candidate shared skill | Would extract | Composable? |
|---|---|---|
| `archigraph-graph-read` | Status → inspect → expand (shipped, #2506) | Yes |
| `archigraph-graph-write` | `archigraph_save_finding` affordance contract (shipped, #2507) | Yes — same "When the user asks to save" section appears in all 8 |
| `archigraph-graph-search` | `archigraph_find` + `archigraph_traces` pattern | Possible — most personas use both |

A skill is worth extracting when: (a) the same prose appears in 3+ persona files, (b) the prose has clear boundaries (a named section), and (c) changing it in one place should change it everywhere.

---

## 10. Telemetry

### 10.1 What is emitted

Each persona calls `archigraph_persona_event` at two lifecycle points:

| Lifecycle point | `event_type` | Required fields | Optional fields |
|---|---|---|---|
| Session start (user hires the persona) | `invoke` | `persona` | `metadata` |
| Consult-Out (user confirms peer engagement) | `consult_out` | `persona`, `target_persona` | `depth`, `chain`, `metadata` |
| Finding persisted via save_finding | `save_finding` | `persona` | `metadata` |

The `save_finding` event_type is available for future use by the `archigraph-graph-write` shared skill; persona bodies currently only emit `invoke` and `consult_out`.

### 10.2 Storage contract

Events are appended to a daily JSONL file:

```
~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl
```

Each line is a JSON object matching the `PersonaEvent` struct in `internal/mcp/persona_telemetry.go`:

```json
{"ts":"2026-05-27T14:03:22Z","persona":"architect","event_type":"invoke"}
{"ts":"2026-05-27T14:07:11Z","persona":"architect","event_type":"consult_out","target_persona":"performance-reviewer"}
```

Files rotate by UTC calendar date. No compaction or deletion is performed by archigraph — the user is responsible for cleanup.

### 10.3 Privacy promise

**LOCAL ONLY.** `archigraph_persona_event` writes exclusively to the local filesystem (`~/.archigraph/events/`). No data is transmitted to any remote endpoint, no aggregation service is contacted, and no identifier beyond the persona name is captured. The `metadata` field is optional and caller-controlled — personas do not populate it with user data. This promise is enforced by the handler implementation in `internal/mcp/persona_telemetry.go` — there is no HTTP client, no gRPC call, and no queue write in that file.

### 10.4 Viewing events

```bash
# Today's events
cat ~/.archigraph/events/persona-events-$(date -u +%Y-%m-%d).jsonl | jq .

# Invoke frequency by persona
cat ~/.archigraph/events/persona-events-*.jsonl | jq -r 'select(.event_type=="invoke") | .persona' | sort | uniq -c | sort -rn

# Consult-Out pairs
cat ~/.archigraph/events/persona-events-*.jsonl | jq -r 'select(.event_type=="consult_out") | "\(.persona) → \(.target_persona)"' | sort | uniq -c | sort -rn
```

A `archigraph personas events --tail` viewer CLI is deferred (see Section 11 Phasing).

### 10.5 Failure behaviour

Telemetry failures (disk full, permissions error, missing HOME) do not surface as errors to the user. The tool returns `{"recorded": false, "warning": "<reason>"}` and the persona continues normally. Personas treat a non-`recorded=true` response as a no-op.

### 10.6 Deferred personas

The four deferred personas (`solutions-architect`, `devops-reviewer`, `compliance-officer`, `dx-engineer`) are NOT yet updated with the Lifecycle telemetry section. When those personas ship, they MUST mirror the pattern exactly as documented in Section 10.1 — the section text is boilerplate and should be copy-pasted with the correct persona name substituted.

---

## 11. Phasing

### v3 (PR #2449 / this doc)

- Architecture doc rewrite (this file).
- `archigraph-consult` SKILL.md rewritten for interactive flow.
- All 8 persona bodies updated: drop fixed OUTPUT, add Communication styles + Consult-Out triggers.
- Cross-platform wrappers: Windsurf workflow + Cursor command (best-effort).

### Deferred

| Item | Reason |
|---|---|
| True multi-persona panel mode | v1/v2 attempted this; postponed until interactive model is validated |
| Persistent active-persona sidecar for Windsurf | Needs design work; conversation-marker workaround ships in this PR |
| Codex / generic-markdown wrappers | Low user demand; defer until requested |
| Persona-emitted findings → graph (opt-in) | **Shipped in #2472** — "When the user asks to save this analysis" section added to all 8 persona bodies; Section 2.4 defines the contract |
| Consult-Out depth > 1 (peer of peer) | **Shipped in #2473** — multi-hop with depth cap (3), cycle detection, and context carry-over. Section 5.4–5.7 define the full protocol. |
| Telemetry on persona usage / Consult-Out frequency | **Shipped in #2474** — `archigraph_persona_event` MCP tool; Section 10 defines the contract and privacy promise |
| Per-persona model selection strategy | **Shipped in #2475** — `model:` frontmatter on all 8 personas with opinionated recommendations; Section 2.3 defines the mapping and override contract |
| Cross-platform renderer CLI | Defer until 3+ platforms stable |
| Solutions-architect / devops / compliance / dx personas | **Shipped in #2451-#2454** — built without original gates met, per user directive. Each documents signal-quality limitations in its persona body. Closing the gate gaps is tracked separately in the personas issue queue. |
| `archigraph personas events --tail` CLI viewer | Deferred; use `jq` one-liners from Section 10.4 in the meantime |

---

## 10. Limitations + honesty contract

When a persona has a known signal-quality limitation, it MUST document it in the persona body via a "## Current-state limitations" section. This is a HARD invariant — users must know when a persona's findings are bounded.

The 4 personas in the 'Phase 1.5 deferred' batch were built without their original gates met (cross_links coverage validation, IaC indexer, data-classification layer, DX hypothesis testing). Each documents this clearly in its body. Closing this gap is tracked as separate work in the personas issue queue.

| Persona | Gate not yet met | Impact on signal quality |
|---|---|---|
| `solutions-architect` | cross_links coverage validation | Limited for single-repo groups; sparse cross-links = incomplete service topology |
| `devops-reviewer` | IaC indexer integration | Cannot review Terraform, Helm, or full k8s manifests; CI/YAML slice only |
| `compliance-officer` | data-classification layer | Name-match heuristics only; no regulatory categorisation; high false-positive rate |
| `dx-engineer` | DX hypothesis testing | Test/import-graph signals only; no docs quality, README, or onboarding-flow review |
