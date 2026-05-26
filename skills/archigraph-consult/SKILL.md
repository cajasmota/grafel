---
name: archigraph-consult
description: Hire a specialist consultant (architect, security-auditor, business-analyst, performance-reviewer, refactor-critic, api-designer, data-engineer, qa-reviewer) to converse with you about the indexed codebase. Interactive, one consultant active at a time, Consult-Out to peers when needed.
when-to-use: User asks to "get a second opinion", "hire an architect", "have someone review this from a security angle", "ask a business analyst about", "review my API design", "check test coverage", or invokes /archigraph-consult explicitly. Hard-depends on /archigraph-tech-docs having been run.
---

# archigraph-consult

Interactive consultant skill. The user **hires** one specialist persona at a time; that persona becomes the active consultant for the conversation and answers the user's questions using the archigraph knowledge graph + generated tech docs as its primary navigation surface.

This is **not** an auto-fan-out skill. It does not run all 8 personas in batch. It does not produce a synthesis report. It does not auto-save findings. See `docs/architecture/personas.md` (v3) for the full paradigm and the reasoning behind the redesign.

## Hard and soft dependencies

| Dependency | Type | Why |
|---|---|---|
| `/archigraph-resolve` | Hard | Orphan edges make impact-radius analysis meaningless |
| `/archigraph-tech-docs` | Hard | Personas read module markdown to ground their answers |
| `/archigraph-business-docs` | Soft | Business-analyst persona fidelity improves |
| `/archigraph-security-audit` | Soft | Security-auditor persona can deduplicate against existing findings |

If tech docs are missing, the skill aborts:
> Tech docs not found at `~/.archigraph/docs/<group>/`. Run `/archigraph-tech-docs` first, then re-invoke `/archigraph-consult`.

## CRITICAL TOOL DISCIPLINE

The active persona MUST use archigraph MCP tools for ALL graph navigation. Pre-flight: call `archigraph_whoami` first.

## When to use this skill

- "Get an architect's view of this codebase."
- "Hire a security auditor to look at our auth flow."
- "I want a business analyst's read on the order module."
- "What would a performance reviewer say about this endpoint?"
- "Review API design consistency."
- "Check test coverage on the billing module."
- `/archigraph-consult` (slash command).

## Flags (optional)

- `--persona <name>` — hire a specific consultant directly without going through catalog selection (e.g. `--persona architect`).
- `--list` / `--catalog` — print the catalog and exit.
- `--release` — release the currently active consultant.
- `--switch <name>` — release the current consultant and hire `<name>`.

## Shipped catalog

Eight consultants ship as persona files in `skills/archigraph-consult/personas/`. Users may add their own under `~/.claude/agents/archigraph-<persona>.md`.

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

## Procedure

### Pre-flight (once per invocation)

1. Call `archigraph_whoami` — confirm group.
2. Verify tech docs exist at `~/.archigraph/docs/<group>/modules/`. If not, abort with the message above.
3. Scan `skills/archigraph-consult/personas/*.md` + `~/.claude/agents/archigraph-*.md` to build the catalog.
4. If `--list` / `--catalog`: print the catalog (name + one-line description from frontmatter) and exit.

### Persona selection

If `--persona <name>` was passed, skip selection and go straight to activation.

Otherwise:

1. Print the catalog (numbered list, with the one-line `description:` from each persona's frontmatter).
2. Ask:
   > Which consultant would you like to hire? You can name one (e.g. "architect") or describe your problem and I'll recommend.
3. If the user describes a problem instead of naming a persona, match the problem to a persona by comparing against the catalog `description:` fields. Confirm the recommendation before activating:
   > Based on your question, I'd recommend hiring **archigraph-<name>**. Proceed?

### Activation

Load the selected persona's `.md` body. Adopt the role in-line in the current conversation:

- **Claude Code:** the main agent reads the persona body and assumes the role for subsequent turns. (Subagents are reserved for Consult-Out — see below.)
- **Windsurf:** the workflow injects `ACTIVE PERSONA: archigraph-<name>` plus the persona body as a system reminder. Cascade adopts the role for subsequent turns.
- **Cursor:** either inline (chat panel) or as a new tab in the Agents Window with the persona as system prompt.

After activation, announce:
> Consultant `archigraph-<name>` is now active. Ask me anything within my lens. Type `/archigraph-consult --release` to release me, or `--switch <name>` to swap consultants.

The active persona runs its own READ instructions (lightweight grounding queries) on activation. Heavier graph queries happen on demand as the user's questions require.

### Conversation

The active persona answers user questions using:
- archigraph MCP tools for navigation
- Tech docs at `~/.archigraph/docs/<group>/modules/`
- The communication styles declared in its body (ASCII diagrams, tables, code samples, analogies, severity matrices — whatever serves the question)

No fixed report shape. Responses are shaped to the question.

### Consult-Out (mid-conversation escalation)

The active persona may, at any time, request a peer's input via a structured callout:

```
> [CONSULT-OUT] I want to bring in archigraph-<peer> because: <reason>
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
archigraph_save_finding(
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
[archigraph-consult] ACTIVE PERSONA: archigraph-<name>
[archigraph-consult] Persona body: <inlined>
```

The main agent re-reads this reminder each turn. If the user's question drifts wildly off-domain, the persona may answer with "that's outside my lens — would you like me to Consult-Out or have you /switch consultant?".

This is not as robust as Claude Code's true subagent isolation. The known failure mode: the user changes topic and the host quietly drops the persona's voice. Mitigation: the user can re-run `/archigraph-consult --persona <name>` to re-anchor. A persistent sidecar is the proper fix and is deferred.

## archigraph MCP tool surface

All personas use: `archigraph_whoami`, `archigraph_find`, `archigraph_inspect`, `archigraph_expand`, `archigraph_traces`, `archigraph_clusters`, `archigraph_stats`.

User-opt-in: `archigraph_save_finding`, `archigraph_list_findings`.

Cross-repo: `archigraph_cross_links`.

## Architecture reference

`docs/architecture/personas.md` — canonical v3 contract: hire-on-demand interactive consultants, Consult-Out, cross-platform delivery matrix, communication styles, anti-patterns.

## Related

- `skills/archigraph-tech-docs/SKILL.md` — hard dependency.
- `skills/archigraph-business-docs/SKILL.md` — soft dependency.
- `skills/archigraph-security-audit/SKILL.md` — soft dependency.
- `~/.claude/agents/` — user-defined custom personas go here.
- `.windsurf/workflows/archigraph-consult.md` — Windsurf wrapper.
- `.cursor/commands/archigraph-consult.md` — Cursor wrapper.

## Read next

→ `/archigraph-help` — overview of the full skill family and suggested entry points.
