---
description: Hire a specialist consultant (architect, security-auditor, business-analyst, performance-reviewer, refactor-critic, api-designer, data-engineer, qa-reviewer) to converse about the indexed archigraph codebase. Interactive — one active consultant at a time, with Consult-Out to peers.
---

# /archigraph-consult (Cursor)

Cursor wrapper for the canonical `archigraph-consult` skill. Persona bodies live at `skills/archigraph-consult/personas/*.md` in the archigraph source tree and are referenced (not duplicated) here.

## Where this runs

- **Chat panel (inline)**: the slash command activates a persona by inlining its body into the current chat. One active consultant per chat thread.
- **Agents Window**: hiring a consultant can open a new agent tab, giving per-tab isolation. Recommended when you want multiple consultants alive simultaneously across tabs.

Either way, the persona body is the system prompt.

## Steps

### 1. Pre-flight

- Call `archigraph_whoami`. If unavailable, ask the user to run `/archigraph-status` and abort.
- Verify `~/.archigraph/docs/<group>/modules/` exists. If not, abort with:
  > Tech docs not found. Run `/archigraph-tech-docs` first, then re-invoke `/archigraph-consult`.

### 2. Catalog

Read every file in `skills/archigraph-consult/personas/*.md`. Extract `name:` and `description:` from frontmatter and present a numbered list. Also scan `~/.claude/agents/archigraph-*.md` for user-defined personas (Cursor reads these by convention when present).

### 3. Hire

Ask the user which consultant to hire — by name or by problem description. If by problem description, recommend a match and confirm.

### 4. Activate

- **Chat panel**: inline the persona body as a system-style turn (`You are now archigraph-<name>. <full body>`). Announce activation. Subsequent turns use this persona.
- **Agents Window**: open a new agent tab with the persona body as system prompt. Pass the user's original question as the first user turn.

Run the persona's READ instructions once at activation to ground.

### 5. Converse

The active consultant answers using archigraph MCP tools + tech docs + the Communication styles declared in its body. No fixed response shape.

### 6. Consult-Out

When the persona emits a `[CONSULT-OUT]` callout and the user approves:
- **Chat panel**: load the peer's body, answer the sub-question with the peer's lens for this turn only, prefix with `[CONSULT-IN: <peer>]`. Then revert to the original consultant.
- **Agents Window**: open a new tab for the peer with carry-over context. Read the peer's answer manually and bring it back.

Cap at 2 Consult-Outs per conversation.

### 7. Release / switch

- `--release`: clear active persona state (chat) or close the tab (Agents Window).
- `--switch <name>`: release current + hire the named persona. The new consultant runs its own READ.

## Cursor rule (recommended)

Drop the following at `.cursor/rules/archigraph-personas.mdc` so Cursor reliably preserves the active-persona contract across turns. (Source-of-truth still lives in `skills/archigraph-consult/personas/`.)

## Saving findings (opt-in only)

`archigraph_save_finding` is invoked only when the user explicitly asks.

## Architecture reference

`docs/architecture/personas.md` — canonical v3 paradigm doc.
