---
description: Hire a specialist consultant (architect, security-auditor, business-analyst, performance-reviewer, refactor-critic, api-designer, data-engineer, qa-reviewer) to converse about the indexed archigraph codebase. Interactive — one active consultant at a time, with Consult-Out to peers.
---

# archigraph-consult (Windsurf)

This is the Windsurf wrapper for the canonical `archigraph-consult` skill. The persona bodies are NOT duplicated here — they live at `skills/archigraph-consult/personas/*.md` in the archigraph source tree. This workflow loads them on demand and prompt-injects the chosen body into Cascade's shared context.

## Windsurf single-context constraint

Cascade has one shared context. There is no subagent isolation. "Hiring" a consultant works by pinning a marker in the conversation and inlining the persona body. The marker stays until released or replaced.

This means:
- Only one active consultant at a time per Cascade thread.
- The user may drift the topic off-domain; if so, Cascade should answer "that's outside my lens — Consult-Out or /switch?" instead of breaking character.
- True isolation requires a sidecar (deferred).

## Steps

### 1. Pre-flight

- Call `archigraph_whoami` via the MCP tool. If unavailable, instruct the user to run `/archigraph-status` first and abort.
- Check `~/.archigraph/docs/<group>/modules/` exists. If not, abort with the message:
  > Tech docs not found. Run `/archigraph-tech-docs` first, then re-invoke `/archigraph-consult`.

### 2. Read the catalog

Read every file matching `skills/archigraph-consult/personas/*.md`. From each, extract the `name:` and `description:` fields from the frontmatter. Also scan `~/.claude/agents/archigraph-*.md` for user-defined personas.

Render the catalog to the user as a numbered list:

```
Available consultants:
 1. archigraph-architect — <description>
 2. archigraph-security-auditor — <description>
 ...
```

### 3. Ask the user to hire one

Ask:
> Which consultant would you like to hire? Name one, or describe your problem and I'll recommend.

If the user describes a problem, match it against the `description:` fields and confirm:
> Based on your question, I'd recommend hiring **archigraph-<name>**. Proceed?

### 4. Activate

Read the chosen persona's body in full. Pin the following marker at the top of Cascade's context:

```
[archigraph-consult] ACTIVE PERSONA: archigraph-<name>
[archigraph-consult] PERSONA BODY FOLLOWS:
<full markdown body of the persona file>
[archigraph-consult] END PERSONA BODY
```

For subsequent turns, Cascade reads this marker each turn and adopts the role. Run the persona's READ instructions once at activation to ground the consultant in the current codebase.

Announce:
> Consultant `archigraph-<name>` is now active. Ask me anything within my lens. Type `/archigraph-consult --release` to release me, or `--switch <name>` to swap.

### 5. Converse

Cascade, adopting the persona, answers user questions using:
- archigraph MCP tools for graph navigation
- Tech docs at `~/.archigraph/docs/<group>/modules/`
- The Communication styles declared in the persona body (ASCII diagrams, tables, code samples, analogies, etc.)

No fixed response shape — answer the question the user asked, in the shape that best serves it.

### 6. Consult-Out (mid-conversation escalation)

When the active persona reaches a sub-question in another consultant's lens, it emits the standard Consult-Out callout (see `skills/archigraph-consult/SKILL.md`). When the user approves:

1. Read the peer persona's body.
2. For the current turn only, append a second marker:
   ```
   [archigraph-consult] CONSULT-OUT TURN: archigraph-<peer>
   [archigraph-consult] PEER PERSONA BODY: <inlined>
   ```
3. Answer the sub-question using both lenses, prefixing the response section that uses the peer's lens with `[CONSULT-IN: <peer>]`.
4. After the answer, the peer's marker expires. The original consultant remains active.

Cap at 2 Consult-Outs per conversation. Beyond that, suggest `--switch <name>`.

### 7. Release / switch

- On `--release`: remove the ACTIVE PERSONA marker, return to neutral.
- On `--switch <name>`: remove the existing marker, repeat steps 4+ with the new persona. The new consultant runs its own READ from scratch.

## Known limitation

Cascade may quietly drop the persona's voice if the user's topic drifts. There is no enforcement layer. The user can re-anchor by re-running `/archigraph-consult --persona <name>`.

## Saving findings (opt-in only)

When the user explicitly asks ("save this finding to the graph"), the active persona calls `archigraph_save_finding`. The workflow does NOT auto-save.

## Architecture reference

`docs/architecture/personas.md` — canonical v3 paradigm doc.
