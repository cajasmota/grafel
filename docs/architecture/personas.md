# Persona Architecture for archigraph

**Status:** Canonical architectural contract
**Scope:** Persona definition, shape, delivery, orchestration, catalog, and phasing.

---

## 1. What is a persona?

A persona is an **agent definition file** — a markdown document that instructs a user's coding agent (Claude Code, Windsurf, Cursor) to adopt a specialist role and analyse a codebase's archigraph knowledge graph and generated documentation from a single, focused perspective. Personas are **not CLI commands**, **not daemon processes**, and **not web-UI features**. They are static files that the user's agent host interprets at invocation time. The persona's output is a structured markdown report and a machine-readable findings list. The `archigraph-consult` skill is the orchestrator that fans out to one or more personas on the user's behalf; the personas themselves are leaf workers — each runs once, produces output, and exits.

---

## 2. Canonical persona shape

### 2.1 Frontmatter

```yaml
---
name: archigraph-<persona-name>           # lowercase, hyphens, prefixed archigraph-
description: >
  One tight sentence: what this persona reviews + when to invoke it.
  Used as the auto-delegation routing key in Claude Code.
tools: Read, Glob, mcp__archigraph__*     # whitelist; no Write unless the persona saves findings
model: sonnet                             # sonnet for Tier A/B; haiku for cheap Tier C passes
---
```

All fields except `model` are required. `tools` must always include `mcp__archigraph__*` (the persona's primary navigation surface) and `Read` (for loading doc files).

### 2.2 Body structure

Every persona body follows this five-section template:

```markdown
## Role

One paragraph: who you are, what you ignore, what you refuse to speculate about without graph evidence.

## READ instructions

Ordered list of data-gathering steps the persona MUST complete before analysis:

1. Call `archigraph_whoami` — confirm group and available repos.
2. Call `archigraph_stats` — get corpus-level metrics (entity count, module count, edge count).
3. Read tech docs at `~/.archigraph/docs/<group>/modules/` — scan `.plan.md` for the module list; read the modules relevant to your focus area.
4. <persona-specific graph queries listed here — archigraph_find, archigraph_inspect, archigraph_expand, archigraph_traces, archigraph_clusters>

## ANALYSIS

The questions this persona MUST answer. Each question maps to a finding section in the output.
No speculation beyond what the graph + docs support. If a question can't be answered from available data, note it as "evidence insufficient" rather than fabricating.

## OUTPUT format

Required sections in the markdown report:

### Summary
3–5 bullets. Plain language. No internal symbol names in the top-level bullets (put symbols in the evidence sub-bullets).

### Findings
One sub-section per finding. Template:
- **Title:** short imperative phrase
- **Severity:** critical | high | medium | low | info
- **Entity refs:** `<entity_id>` (link to graph node)
- **Evidence:** quoted snippet or graph path that proves the finding
- **Recommendation:** concrete action (what, not how)
- **Confidence:** 0.0–1.0; must be < 0.7 if evidence is indirect

JSON finding record (one per finding, emitted alongside the report):
```json
{
  "title": "...",
  "severity": "high",
  "entity_id": "...",
  "persona": "<name>",
  "confidence": 0.85,
  "recommendation": "...",
  "blast_radius": "..."
}
```

### Deferred / insufficient evidence
Findings the persona wanted to raise but could not substantiate from the graph + docs. Lists the question, what evidence was sought, and what was missing.

## STOP criteria

The persona MUST stop and return its report when ANY of the following are true:
- All ANALYSIS questions have been answered or deferred.
- More than 15 findings have been emitted (cap per persona to avoid noise floods).
- Tech docs are missing for a module required by a question (escalate to "deferred").
- The user's agent requests early termination.
```

### 2.3 Compose-skills pattern

A persona does not need to duplicate logic shared across multiple personas. Common capabilities live in separate archigraph skills that the persona's frontmatter preloads:

```yaml
skills:
  - archigraph-graph-read          # MCP query patterns and tool discipline
  - archigraph-finding-formatter   # standard finding shape + JSON serialiser
```

This keeps persona files focused on the "what to look for" rather than "how to query the graph". In this PR, the shared skills are inline (the persona body contains both role and query instructions); extracting them into discrete skill files is deferred to a followup (see Section 7).

---

## 3. Cross-platform delivery

### 3.1 Canonical target: Claude Code subagent

The canonical persona definition is a **Claude Code subagent** file at:

```
~/.claude/agents/archigraph-<name>.md          # personal install
.claude/agents/archigraph-<name>.md            # project-scoped install
```

This is the canonical target because:
- Claude Code provides isolated context windows per subagent (no persona cross-contamination).
- The `skills:` frontmatter field allows clean skill composition.
- The `description:` field drives automatic delegation — the `archigraph-consult` skill matches the user's intent to the right persona without the user naming it explicitly.
- The user base (confirmed from environment) primarily uses Claude Code.

The persona files shipped in `skills/archigraph-consult/personas/` are the Claude Code canonical definitions. The `archigraph-consult` skill installs them by loading them as subagent instructions when fanning out.

### 3.2 Windsurf (secondary target, deferred)

Windsurf workflows live at `.windsurf/workflows/<name>.md`. They run in Cascade's single shared context (no isolation per persona). Personas on Windsurf must run sequentially and be capped at 12,000 characters each. The Windsurf wrapper can be generated from the canonical Claude Code definition by stripping the YAML frontmatter and converting the five-section body into a numbered Cascade step chain. Generation tooling is deferred (see Section 7).

### 3.3 Cursor (secondary target, deferred)

Cursor commands live at `.cursor/commands/<name>.md`. Cursor 3.0's Agents Window supports up to 8 parallel agents. Persona slash commands can be generated from the canonical definition. Generation tooling is deferred (see Section 7).

### 3.4 Choosing one over another

| Concern | Claude Code | Windsurf | Cursor |
|---|---|---|---|
| Context isolation per persona | Yes | No (shared) | Yes (Agent tab) |
| Skill composition | Yes (`skills:`) | No | Partial (rules) |
| Auto-delegation | Yes (`description:`) | No (manual `/`) | No (manual) |
| Parallel persona fan-out | Native | Sequential only | Up to 8 |
| **Recommended for archigraph** | **Primary** | Secondary | Secondary |

---

## 4. Orchestration via `archigraph-consult`

The user never invokes a persona directly. They invoke the `archigraph-consult` **skill** (via `/archigraph-consult` slash command or by natural-language request). The skill orchestrates:

1. **Pre-flight**: calls `archigraph_whoami`, verifies tech docs exist, loads the persona catalog from `skills/archigraph-consult/personas/*.md`.
2. **Persona selection**: matches the user's intent (or `--persona <name>` flag) against the `description:` field of each persona file.
3. **Fan-out**: for each selected persona, the skill spawns a subagent using the persona's `.md` body as the system prompt. In Claude Code this is a true isolated subagent. On Windsurf it's a sequential workflow step.
4. **Collection**: each subagent returns a markdown report + JSON findings list. The skill writes them to `~/.archigraph/consultations/<session-id>/`.
5. **Editor synthesis**: after all personas complete, an editor pass deduplicates findings raised by multiple personas and ranks by cross-persona agreement.
6. **Graph materialisation**: findings with `confidence >= 0.7` are written to the graph via `archigraph_save_finding`.
7. **Session summary**: prints a one-screen summary and the session path.

### 4.1 Multi-persona fan-out flow

```
User: /archigraph-consult
  └─ archigraph-consult skill
       ├─ spawn: archigraph-architect      → architect-report.md + findings.json
       ├─ spawn: archigraph-security-auditor → security-report.md + findings.json
       ├─ spawn: archigraph-performance-reviewer → perf-report.md + findings.json
       ├─ spawn: archigraph-refactor-critic   → refactor-report.md + findings.json
       └─ spawn: archigraph-business-analyst  → ba-report.md + findings.json
            │
            ▼
       editor pass: synthesis.md (deduplicated, ranked)
            │
            ▼
       archigraph_save_finding (confidence >= 0.7)
```

The skill (not the persona) decides which personas run. Personas do not communicate with each other during a run.

---

## 5. Persona catalog

Archigraph ships the following personas. This PR delivers all marked **Phase 1**; the rest are deferred.

| # | Name | Role | Primary graph queries | Phase |
|---|---|---|---|---|
| 1 | `architect` | Internal structure — layering, coupling, cyclic deps, god modules, boundary violations | `archigraph_clusters`, `archigraph_expand` (IMPORTS/CALLS), `archigraph_stats` | Phase 1 (enhanced) |
| 2 | `security-auditor` | Auth gaps, PII exposure, injection risks, secrets, attack surface | `archigraph_traces` (auth entry points), `archigraph_expand` (CALLS from user-input handlers), `archigraph_find` (credential patterns) | Phase 1 (enhanced) |
| 3 | `business-analyst` | Capability coverage, feature gaps, business rule completeness, user-journey gaps | `archigraph_traces` (route → handler → service), `archigraph_find` (route entities), `archigraph_clusters` | Phase 1 (enhanced) |
| 4 | `performance-reviewer` | Hot paths, N+1 queries, synchronous blocking, unbounded queries, over-fetching | `archigraph_expand` (CALLS depth), `archigraph_traces` (high-traffic entry points), `archigraph_find` (DB call patterns) | Phase 1 (enhanced) |
| 5 | `refactor-critic` | Complexity hotspots, duplication, dead code, long call chains, tech-debt | `archigraph_stats`, `archigraph_expand` (zero-caller nodes), `archigraph_clusters` (high-degree nodes) | Phase 1 (enhanced) |
| 6 | `api-designer` | Endpoint naming, REST/RPC convention consistency, versioning, OpenAPI gaps, error-shape uniformity | `archigraph_find` (http_endpoint entities), `archigraph_inspect` (route handler chains), `archigraph_cross_links` | Phase 1 (new) |
| 7 | `data-engineer` | Schema quality, migration hygiene, ORM query patterns, missing indexes, FK integrity | `archigraph_find` (schema/model entities), `archigraph_expand` (CALLS from ORM layers), `archigraph_traces` | Phase 1 (new) |
| 8 | `qa-reviewer` | Test coverage by module, missing test types, untested critical paths, fixture hygiene | `archigraph_expand` (TESTS edges), `archigraph_find` (test entities), `archigraph_traces` (critical paths) | Phase 1 (new) |
| 9 | `solutions-architect` | System-level fit — external integration points, third-party SDKs, data flow across services, deployment topology | `archigraph_cross_links`, `archigraph_find` (external API clients), `archigraph_clusters` | Deferred |
| 10 | `devops-reviewer` | Deployability, IaC quality, env var contracts, observability, 12-factor compliance | `archigraph_find` (config/env entities), `archigraph_expand` (startup chains) | Deferred — archigraph is a code-graph indexer; IaC analysis requires separate tooling |
| 11 | `compliance-officer` | GDPR PII handling, audit trails, dependency licenses, SOC2 access controls | `archigraph_find` (PII-adjacent entities), `archigraph_expand` (data flow), `archigraph_traces` | Deferred — high false-positive risk without semantic data-classification |
| 12 | `dx-engineer` | Onboarding friction, error messages, dev-loop timing | `archigraph_find` (entry points), `archigraph_stats` | Deferred — limited graph signal; better as a human review |
| 13 | `editor` (synthesis) | Deduplicates + ranks findings from all other personas | Reads persona outputs; no direct graph queries | Phase 1 (in skill; not a standalone persona file) |

**Dropped from catalog:**

- **Accessibility Auditor** — WCAG/ARIA analysis requires DOM/rendering context that archigraph does not index. Static call-graph analysis produces near-zero signal. Drop entirely; not deferred.
- **Tech Writer** — reviewing archigraph's own generated docs is circular (the persona reads the docs to judge the docs). Better addressed by a separate quality-check pass (`/archigraph-graph-quality`). Drop entirely.
- **Devil's Advocate** — useful concept but not a standalone persona; better implemented as an optional argument to the editor synthesis pass. Deferred to the editor persona followup.

---

## 6. What personas do NOT do

This section is non-negotiable. Any implementation that violates these invariants is wrong.

- **No CLI invocation.** There is no `archigraph architect` or `archigraph consult start` command that spawns a persona. The CLI has no persona subcommand. Personas are markdown files, not CLI subcommands.
- **No daemon spawn.** Personas do not run as background processes. They execute once within the user's agent host's context and terminate.
- **No web-UI surface.** Personas do not appear in the archigraph web dashboard as runnable items. The dashboard may show *findings* that personas emitted (via `archigraph_save_finding`), but it has no concept of "run persona".
- **No MCP-tool-registry membership.** Personas are not MCP tools. They consume MCP tools (`mcp__archigraph__*`); they are not exposed as MCP endpoints themselves.
- **No budget-management infrastructure in personas.** Token/cost budgeting is a concern of the `archigraph-consult` skill and the user's agent host, not of individual persona files. Persona files do not contain budget caps, model-selection logic for cost reasons, or spending meters.
- **No cross-persona communication during a run.** Personas are stateless workers. They do not read each other's in-progress output. The editor synthesis pass (run by the skill after all personas complete) is where cross-persona reasoning happens.
- **No install CLI.** There is no `archigraph personas install --target=claude-code` command in this PR. Installation is handled by the skill loading persona files directly. A cross-platform renderer CLI is deferred.

---

## 7. Phasing

### This PR (Phase 1)

- Architecture doc (this file) — the contract.
- 5 existing personas enhanced to canonical shape: `architect`, `security-auditor`, `business-analyst`, `performance-reviewer`, `refactor-critic`.
- 3 new personas added: `api-designer`, `data-engineer`, `qa-reviewer`.
- `skills/archigraph-consult/SKILL.md` updated to reflect the full 8-persona set and reference this doc.

### Deferred (Phase 2 and beyond)

| Item | Reason for deferral |
|---|---|
| `solutions-architect` persona | Needs cross-repo topology tooling; `archigraph_cross_links` coverage must be validated first |
| `devops-reviewer` persona | IaC analysis outside archigraph's graph scope; requires separate static-analysis integration |
| `compliance-officer` persona | High false-positive rate without semantic data classification; needs `archigraph-pii-scanner` skill first |
| `dx-engineer` persona | Low graph signal; design work needed to define measurable graph-derived metrics |
| Editor persona as standalone subagent | Currently implemented inline in the skill's "editor synthesis" step; extracting to a full persona file deferred until the finding deduplication schema is stable |
| Cross-platform renderer CLI (`archigraph personas install`) | Depends on stable canonical format; defer until 3+ platforms are validated manually |
| Windsurf workflow wrappers | Secondary target; defer until Claude Code path is stable and adoption warrants it |
| Cursor command wrappers | Secondary target; same rationale as Windsurf |
| Multi-agent fan-out runtime infrastructure | True parallel subagent orchestration with TUI progress grid deferred; sequential fan-out in the skill is sufficient for Phase 1 |
| Findings → graph entity schema (Finding + CrossReference typed nodes) | Schema design in progress; `archigraph_save_finding` covers the interim need |
| Persona skill extraction (`archigraph-graph-read`, `archigraph-finding-formatter` as discrete skills) | Inline in persona bodies for now; extract when 3+ personas duplicate the same boilerplate |
| Interactive resume sessions (`archigraph consult resume`) | State management and locking design needed; not in scope for this PR |
| Per-persona model selection strategy | Haiku vs Sonnet vs Opus decision belongs in the skill, not persona files; deferred to skill iteration |
