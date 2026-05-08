# ADR-008: Caller-CWD aware routing for multi-group setups

- **Status**: Accepted
- **Date**: 2026-05-08
- **Deciders**: Jorge Cajas

## Context

ADR-004 establishes that a single MCP process per machine serves every registered group. That decision shifts a question onto every tool call: **which group is this call about?** With ten groups registered, a tool like `search("how does authentication work?")` is ambiguous unless the caller specifies a scope.

Two failure modes to avoid:

1. **Burdening the agent.** Requiring the agent to pass an explicit `group` argument on every call is friction the agent will sometimes get wrong, and adds noise to every tool invocation.
2. **Silent guessing.** Picking a default group when the answer is genuinely ambiguous gives the agent confidently wrong answers.

The MCP protocol exposes some session metadata to servers, including, in many host integrations, the caller's working directory. Most agents working in a registered repo are doing so from a CWD inside that repo. If we can read that signal reliably, the common case becomes friction-free.

## Decision

archigraph implements a three-step resolution cascade for every tool call that needs a group:

1. **Explicit argument first.** If the caller passes `group=<name>`, use that group. No further checks. This is the override path for cross-group queries and for agents that know exactly what they want.

2. **CWD inference second.** If no `group` argument is present, examine the caller's session metadata for a working directory. Walk that directory upward looking for a `.archigraph/group.json` marker file. If found, the marker declares the owning group; use it. The marker is created by `archigraph register` and is committed (or gitignored, user's choice — defaults to gitignored).

3. **Singleton fallback third.** If steps 1 and 2 fail and the registry holds exactly one group, use that group. Otherwise, return a structured error explaining the ambiguity and listing registered groups, so the agent can re-issue with an explicit `group` argument.

A new `whoami()` MCP tool exposes the inferred group + repo + resolution-source for the current caller session. Agents can call `whoami` for self-orientation when they are uncertain, and the tool's response is itself a teaching signal about how routing works.

## Consequences

### Positive
- The common case ("agent is working inside a registered repo") requires no extra arguments; the agent calls `search` or any other tool naturally and routing happens silently.
- Cross-group queries are explicit and unambiguous via the `group` argument.
- The error path is informative rather than guessing; agents fail loudly and recover correctly.
- `whoami` is a small tool that pays back many agent-side debugging conversations.

### Negative
- Reliable CWD propagation depends on the host integration. Agents whose hosts do not pass CWD metadata fall through to step 3 and may hit the ambiguity error more often. Mitigation: the error message tells them exactly what to do.
- The `.archigraph/group.json` marker introduces a small per-repo file. Users who dislike this can use the explicit `group` argument and skip the marker.
- Walking up directories adds a few microseconds per call; negligible.

### Neutral
- The cascade is a documented contract in `SCHEMA.md` and is part of the MCP server's behavioral spec from ADR-002.
- This routing model presumes the single-process architecture from ADR-004; in a per-project model the question would not arise.

## Alternatives considered

- **Mandatory `group` argument on every tool** — rejected: too much friction for the common case; agents will forget or get it wrong.
- **Default to "first registered group"** — rejected: silent guessing; produces confidently wrong answers when the user has multiple groups.
- **Session-bound group selection** (set once per MCP session via a `set_group` call) — rejected: stateful per-session affinity is brittle across reconnects and conflicts with the agent's natural CWD-driven workflow.
- **Infer group from the first symbol referenced in the query** — rejected: too magical, often ambiguous (the same symbol name may exist in multiple groups), and indistinguishable from a real cross-group query.
