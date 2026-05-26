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

The active consultant signals the need with a structured callout in their response:

```
> [CONSULT-OUT] I want to bring in archigraph-performance-reviewer because:
> - The handler at entity_id `auth.LoginHandler` does a synchronous DB scan
>   (see archigraph_expand result above)
> - Latency optimisation is outside my (security) lens
>
> Context to carry over:
> - Entity under discussion: auth.LoginHandler (entity_id: 4abf…)
> - The user's original question: "is this login flow safe?"
> - What I've found so far: <3-bullet summary>
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
- The user's original question.
- A 2–4 bullet summary of what the original consultant has found so far.
- The specific sub-question the peer is being asked to answer.

This avoids the peer re-doing the original consultant's READ phase.

### 5.3 When NOT to Consult-Out

- The peer's lens overlaps trivially with the active consultant's — handle it inline.
- The user has not yet engaged deeply with the active consultant's answer.
- More than 2 Consult-Outs have already happened in this conversation (panel sprawl).

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

Eight personas ship. The catalog count must match across this doc, `SKILL.md`, and the filesystem at `skills/archigraph-consult/personas/`.

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

**Deferred** (documented but not shipped): `solutions-architect`, `devops-reviewer`, `compliance-officer`, `dx-engineer`. Rationale unchanged from PR #2449.

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

## 9. Phasing

### This PR (v3)

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
| Consult-Out depth > 1 (peer of peer) | Single hop only in v3 |
| Telemetry on persona usage / Consult-Out frequency | Needs privacy review |
| Per-persona model selection strategy | **Shipped in #2475** — `model:` frontmatter on all 8 personas with opinionated recommendations; Section 2.3 defines the mapping and override contract |
| Cross-platform renderer CLI | Defer until 3+ platforms stable |
| Solutions-architect / devops / compliance / dx personas | As per PR #2449 deferral reasons |
