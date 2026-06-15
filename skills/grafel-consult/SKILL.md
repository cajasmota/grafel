---
name: grafel-consult
description: Hire a specialist consultant (architect, security-auditor, business-analyst, performance-reviewer, refactor-critic, api-designer, data-engineer, qa-reviewer) to converse with you about the indexed codebase. Interactive, one consultant active at a time, Consult-Out to peers when needed.
when-to-use: User asks to "get a second opinion", "hire an architect", "have someone review this from a security angle", "ask a business analyst about", "review my API design", "check test coverage", or invokes /grafel-consult explicitly. Hard-depends on /grafel-tech-docs having been run.
---

# grafel-consult

Interactive consultant skill. The user **hires** one specialist persona at a time; that persona becomes the active consultant for the conversation and answers the user's questions using the grafel knowledge graph + generated tech docs as its primary navigation surface.

This is **not** an auto-fan-out skill. It does not run all 8 personas in batch. It does not produce a synthesis report. It does not auto-save findings. See `docs/architecture/personas.md` (v3) for the full paradigm and the reasoning behind the redesign.

## Hard and soft dependencies

| Dependency | Type | Why |
|---|---|---|
| `/grafel-resolve` | Hard | Orphan edges make impact-radius analysis meaningless |
| `/grafel-tech-docs` | Hard | Personas read module markdown to ground their answers |
| `/grafel-business-docs` | Soft | Business-analyst persona fidelity improves |
| `/grafel-security-audit` | Soft | Security-auditor persona can deduplicate against existing findings |

If tech docs are missing, the skill aborts:
> Tech docs not found at `~/.grafel/docs/<group>/`. Run `/grafel-tech-docs` first, then re-invoke `/grafel-consult`.

## CRITICAL TOOL DISCIPLINE

The active persona MUST use grafel MCP tools for ALL graph navigation. Pre-flight: call `grafel_whoami` first.

## When to use this skill

- "Get an architect's view of this codebase."
- "Hire a security auditor to look at our auth flow."
- "I want a business analyst's read on the order module."
- "What would a performance reviewer say about this endpoint?"
- "Review API design consistency."
- "Check test coverage on the billing module."
- `/grafel-consult` (slash command).

## Flags (optional)

- `--persona <name>` — hire a specific consultant directly without going through catalog selection (e.g. `--persona architect`).
- `--list` / `--catalog` — print the catalog and exit.
- `--release` — release the currently active consultant.
- `--switch <name>` — release the current consultant and hire `<name>`.
- `--resume <session-id>` — resume a specific saved session by ID (skips the session picker).
- `--new` — force a new session, skipping the active-session list.

## Shipped catalog

Twelve consultants ship as persona files in `skills/grafel-consult/personas/`. Users may add their own under `~/.claude/agents/grafel-<persona>.md`.

| Consultant | File | Lens |
|---|---|---|
| architect | `personas/architect.md` | Module layering, coupling, cyclic deps, god modules, ADR opportunities |
| security-auditor | `personas/security-auditor.md` | Auth gaps, PII exposure, injection risks, attack surface |
| business-analyst | `personas/business-analyst.md` | Capability coverage, feature gaps, business rule completeness, user-journey gaps |
| performance-reviewer | `personas/performance-reviewer.md` | Hot paths, N+1 queries, synchronous blocking, unbounded queries |
| refactor-critic | `personas/refactor-critic.md` | Complexity hotspots, duplication, dead code, tech-debt surface |
| api-designer | `personas/api-designer.md` | Endpoint naming, REST/RPC convention consistency, versioning, error-shape uniformity |
| data-engineer | `personas/data-engineer.md` | Schema quality, migration hygiene, ORM query patterns, index candidates, FK integrity |
| qa-reviewer | `personas/qa-reviewer.md` | TESTS-edge coverage by module, untested critical paths, fixture hygiene |
| solutions-architect | `personas/solutions-architect.md` | Cross-service boundaries, inter-repo contracts, coupling, blast-radius (limited: cross_links coverage) |
| devops-reviewer | `personas/devops-reviewer.md` | CI/CD config, GitHub Actions pinning, build hygiene, graph-visible infra config (limited: no IaC indexer) |
| compliance-officer | `personas/compliance-officer.md` | PII field detection, audit-trail gaps, sensitive data flow surface scan (limited: name-match heuristics only) |
| dx-engineer | `personas/dx-engineer.md` | Test deserts, circular imports, god entry-points, module size outliers (limited: test/import-graph signals only) |

## Session state

Session state is persisted to `~/.grafel/sessions/<session-id>.yaml` using the host agent's `Read` and `Write` tools. No new MCP tools are required — all personas inherit `Read` and `Write` from the host agent.

### Schema

```yaml
session_id: <uuid-or-timestamp-slug>        # e.g. "20260527-143022-architect"
created: <iso8601>                           # e.g. "2026-05-27T14:30:22Z"
last_active: <iso8601>                       # updated on every save
active_persona: <name>                       # e.g. "architect"
consult_chain: [persona-a, persona-b]        # active Consult-Out chain at save time
context:
  original_ask: "<verbatim user question>"
  prior_findings:
    - persona: <name>
      summary: "<2-4 line text summary of that hop's findings>"
notes: |
  <free-form scratch — any persona may append mid-conversation>
```

### First-invocation flow (session picker)

On every invocation (unless `--new` or `--resume <id>` is passed):

1. Scan `~/.grafel/sessions/*.yaml` for files whose `last_active` is within the past 30 days.
2. If any active sessions exist, present them:
   ```
   Active sessions:
     [1] 20260527-143022-architect    last active: 2026-05-27  persona: architect
     [2] 20260525-091500-security     last active: 2026-05-25  persona: security-auditor
     [N] Start new session
   ```
   Ask: "Resume a session, or start new?"
3. If no active sessions exist (or the user picks "Start new session"), proceed to Persona selection below.

### Resume flow

When the user picks an existing session (or passes `--resume <session-id>`):

1. Read `~/.grafel/sessions/<session-id>.yaml`.
2. Re-prime the host agent:
   - Set `active_persona` to the saved value.
   - Inject the saved `context.original_ask`, `prior_findings`, `consult_chain`, and `notes` into the conversation as a system reminder block:
     ```
     [grafel-consult] RESUMING SESSION <session-id>
     Active persona: grafel-<name>
     Original ask: <original_ask>
     Prior findings: <prior_findings as bullet list>
     Consult chain: <consult_chain>
     Notes: <notes>
     ```
3. Activate the persona (load its body inline, same as a new hire).
4. Announce to the user:
   > Resumed session `<session-id>` with consultant `grafel-<name>`. Context restored. Pick up where you left off.

### Save flow

Session state is written under two conditions:

1. **Explicit user request** — user says "save session", "checkpoint this", "save my place", or equivalent.
2. **On Consult-Out** — when the active persona emits a `[CONSULT-OUT]` block and the user approves, the skill writes current state before spawning the peer (so the carry-over context is recoverable if the peer chain is interrupted).

To save, the persona uses the host agent's `Write` tool:
- Path: `~/.grafel/sessions/<session-id>.yaml`
- Content: full YAML schema above, with `last_active` updated to the current UTC time.

The session directory `~/.grafel/sessions/` is created if it does not exist (use `Bash` with `mkdir -p`).

### End / archive policy

- **Explicit end**: user says `[END SESSION]` or runs `/grafel-consult --release` and confirms archiving. The YAML is moved to `~/.grafel/sessions/archive/<session-id>.yaml`.
- **Staleness auto-archive**: sessions whose `last_active` is more than 30 days ago are shown in the session picker with a `[stale]` label. Selecting a stale session prompts: "This session is >30 days old. Resume anyway, or archive it?" Archiving moves it to `~/.grafel/sessions/archive/`.
- The `archive/` directory is never auto-deleted. The user is responsible for cleanup.

## Procedure

### Pre-flight (once per invocation)

1. Call `grafel_whoami` — confirm group.
2. Verify tech docs exist at `~/.grafel/docs/<group>/modules/`. If not, abort with the message above.
3. Scan `skills/grafel-consult/personas/*.md` + `~/.claude/agents/grafel-*.md` to build the catalog.
4. If `--list` / `--catalog`: print the catalog (name + one-line description from frontmatter) and exit.
5. **Session picker**: unless `--new` or `--resume <id>` was passed, scan `~/.grafel/sessions/` and present active sessions (see Session state above). If the user resumes, skip Persona selection and go straight to Activation.

### Persona selection

If `--persona <name>` was passed, skip selection and go straight to activation.

Otherwise:

1. Print the catalog (numbered list, with the one-line `description:` from each persona's frontmatter).
2. Ask:
   > Which consultant would you like to hire? You can name one (e.g. "architect") or describe your problem and I'll recommend.
3. If the user describes a problem instead of naming a persona, match the problem to a persona by comparing against the catalog `description:` fields. Confirm the recommendation before activating:
   > Based on your question, I'd recommend hiring **grafel-<name>**. Proceed?

### Activation

Load the selected persona's `.md` body. Adopt the role in-line in the current conversation:

- **Claude Code:** the main agent reads the persona body and assumes the role for subsequent turns. (Subagents are reserved for Consult-Out — see below.)
- **Windsurf:** the workflow injects `ACTIVE PERSONA: grafel-<name>` plus the persona body as a system reminder. Cascade adopts the role for subsequent turns.
- **Cursor:** either inline (chat panel) or as a new tab in the Agents Window with the persona as system prompt.

After activation, announce:
> Consultant `grafel-<name>` is now active. Ask me anything within my lens. Type `/grafel-consult --release` to release me, or `--switch <name>` to swap consultants.

The active persona runs its own READ instructions (lightweight grounding queries) on activation. Heavier graph queries happen on demand as the user's questions require.

### Conversation

The active persona answers user questions using:
- grafel MCP tools for navigation
- Tech docs at `~/.grafel/docs/<group>/modules/`
- The communication styles declared in its body (ASCII diagrams, tables, code samples, analogies, severity matrices — whatever serves the question)

No fixed report shape. Responses are shaped to the question.

### Consult-Out (mid-conversation escalation)

The active persona may, at any time, request a peer's input via a structured callout:

```
> [CONSULT-OUT] I want to bring in grafel-<peer> because: <reason>
>
> Carry-over context:
> - Entity/entities under discussion: <entity_ids>
> - User's original question: <quoted>
> - What I've found so far: <2-4 bullets>
> - Specific sub-question for the peer: <one sentence>
>
> Shall I bring them in?
```

When the user approves:

- **Save session first**: before spawning the peer, write the current session state to `~/.grafel/sessions/<session-id>.yaml` (see Session state → Save flow). This ensures the Consult-Out carry-over context is recoverable if the chain is interrupted.
- **Claude Code:** spawn the peer as a subagent via the Task tool, with the carry-over context as the opening message and the peer's persona body as system prompt. The peer answers the sub-question and returns. Summarise back to the user prefixed `[CONSULT-IN: <peer>]`. The original consultant remains active in the parent conversation.
- **Windsurf:** for the current turn only, layer the peer's lens onto Cascade by inlining the peer's body alongside the original. After the turn, the peer's marker expires; the original consultant remains.
- **Cursor:** open a new Agents Window tab with the peer, passing carry-over context. Bring the answer back to the original tab manually or via paste.

Cap Consult-Outs at 2 per conversation to prevent panel sprawl. Beyond that, suggest the user switch consultants instead.

### Release / switch

- `--release` — clear the active persona. The skill returns to neutral mode.
- `--switch <name>` — release current + activate the named persona. Note that the new consultant does NOT inherit conversation history beyond what the user re-states; they re-run their own READ.

### Saving findings (opt-in)

If the user explicitly asks ("save this finding to the graph"), the active persona calls:

```
grafel_save_finding(
  type="consultant_finding",
  question="<persona>: <finding_title>",
  answer="<explanation>",
  entity_id="<entity_id if applicable>"
)
```

The skill does NOT auto-save. v1/v2's `confidence >= 0.7` auto-save behaviour is retired.

## Active-persona state on platforms without isolation

On Windsurf (and any host without per-subagent context), "active persona" is tracked **in the conversation itself** via a system reminder pinned at the top of context:

```
[grafel-consult] ACTIVE PERSONA: grafel-<name>
[grafel-consult] Persona body: <inlined>
```

The main agent re-reads this reminder each turn. If the user's question drifts wildly off-domain, the persona may answer with "that's outside my lens — would you like me to Consult-Out or have you /switch consultant?".

This is not as robust as Claude Code's true subagent isolation. The known failure mode: the user changes topic and the host quietly drops the persona's voice. Mitigation: the user can re-run `/grafel-consult --persona <name>` to re-anchor. A persistent sidecar is the proper fix and is deferred.

## grafel MCP tool surface

All personas use: `grafel_whoami`, `grafel_find`, `grafel_inspect`, `grafel_expand`, `grafel_traces`, `grafel_clusters`, `grafel_stats`.

User-opt-in: `grafel_save_finding`, `grafel_list_findings`.

Cross-repo: `grafel_cross_links`.

## Architecture reference

`docs/architecture/personas.md` — canonical v3 contract: hire-on-demand interactive consultants, Consult-Out, cross-platform delivery matrix, communication styles, anti-patterns.

## Related

- `skills/grafel-tech-docs/SKILL.md` — hard dependency.
- `skills/grafel-business-docs/SKILL.md` — soft dependency.
- `skills/grafel-security-audit/SKILL.md` — soft dependency.
- `~/.claude/agents/` — user-defined custom personas go here.
- `.windsurf/workflows/grafel-consult.md` — Windsurf wrapper.
- `.cursor/commands/grafel-consult.md` — Cursor wrapper.

## Read next

→ `/grafel-help` — overview of the full skill family and suggested entry points.
