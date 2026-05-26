---
name: archigraph-security-audit
description: Two-phase security audit of the indexed group. Phase 1 is deterministic static analysis (runs inside the indexer via internal/security/). Phase 2 is LLM-driven semantic confirmation, ranking, and explanation — adds SecurityFinding entities to the graph and emits a security/ doc tier. Interactive by default; --auto for CI.
when-to-use: User asks to "audit security", "find security issues", "run the security check", "check for auth gaps", or invokes /archigraph-security-audit explicitly. Best run after /archigraph-resolve (orphan endpoints produce false negatives in auth coverage). /archigraph-tech-docs output is optional soft context for Phase 2.
---

# archigraph-security-audit

Run a two-phase security audit against the indexed archigraph group.

- **Phase 1 — Static analysis** (`prompts/01-static-analysis.md`): deterministic rule-based checks run against the graph. Finds missing auth decorators, publicly reachable endpoints with no permission check, orphan routes, exposed PII fields, and other structural patterns. Essentially free — no LLM calls.
- **Phase 2 — LLM semantic pass** (`prompts/02-semantic-confirmation.md`): LLM-driven confirmation, ranking, and explanation. Confirms or dismisses Phase 1 findings, adds severity scores, writes human-readable explanations, and surfaces findings the static pass missed. Interactive by default — the user reviews findings before they are submitted to the graph.

## When to use this skill

- "Audit security for this group."
- "Find auth gaps / missing permission checks."
- "Run a security check before the release."
- `/archigraph-security-audit` (slash command).

Do **not** invoke for code quality, documentation, or enrichment — those are separate skills.

## Prerequisites

- A running archigraph daemon with the group indexed.
- Recommended: run `/archigraph-resolve` first — orphan endpoints (unresolved edges) produce false-negative auth coverage results.
- Optional (improves Phase 2 fidelity): `/archigraph-tech-docs` output — the LLM gets module README context so it understands what a handler is supposed to do before judging whether it has adequate auth.

## CRITICAL TOOL DISCIPLINE

Use archigraph MCP tools for ALL graph navigation: `archigraph_whoami`, `archigraph_find`, `archigraph_inspect`, `archigraph_expand`, `archigraph_traces`. Do NOT grep source for structural questions.

### Pre-flight assertion

Call `archigraph_whoami` first. If it errors: ABORT with "archigraph MCP not configured. Run `/mcp` to fix."

## Cost and mode

| Phase | LLM | Cost | Default |
|-------|-----|------|---------|
| Phase 1 (static) | None | ~$0 | Always runs |
| Phase 2 (semantic) | Haiku (most findings), Sonnet (critical) | ~100k–500k tokens | Interactive by default |

**`--auto` flag:** skips interactive confirmation in Phase 2; all findings above `confidence=0.7` are auto-submitted to the graph. Use for CI pipelines.
**`--phase1-only` flag:** run only Phase 1 and emit the static report without LLM Phase 2.
**`--since <sha>` flag:** restrict Phase 1 to entities changed since the given commit (delta mode for CI gates).

## Phase 1 — Static analysis

Follow `prompts/01-static-analysis.md`. Key checks:

1. **Auth coverage** — for every `http_endpoint` entity, check whether the graph contains an `AUTHENTICATED_BY` or `REQUIRES_PERMISSION` edge. Endpoints with no such edge are flagged `auth_missing`.
2. **Reachability** — identify endpoints reachable from the public internet (no internal-only marker) with `auth_missing`. These become `severity=high` findings.
3. **Orphan endpoints** — endpoints with no callers AND no auth edge. Could be dead code or unauthenticated surface.
4. **PII exposure** — entities of kind `DataField` tagged `pii=true` that are returned by an unauthenticated endpoint.
5. **Overly broad permissions** — permission checks that resolve to `*` or `admin_only` when the endpoint clearly only needs a scoped permission.
6. **Residual edges on security-sensitive paths** — any endpoint that has an unresolved outbound edge (detected via `archigraph_repairs(action=list)`) on a path marked as authentication-related.

Each finding gets a fingerprint: `sha256(entity_id + check_name)`. The fingerprint is stable across re-runs so re-running doesn't create duplicate findings.

Output: `~/.archigraph/groups/<group>/archigraph-security-audit/phase1-findings.json`.

## Phase 2 — LLM semantic pass

Follow `prompts/02-semantic-confirmation.md`. For each Phase 1 finding:

1. Load the entity's `archigraph_expand(node=<id>, depth=2)` for neighbour context.
2. If `/archigraph-tech-docs` output exists for the entity's module, load the relevant section.
3. Ask the LLM: is this finding a real security issue, a false positive, or needs-more-info? Assign `severity` (critical/high/medium/low/info) and write a human-readable `explanation`.
4. Present to the user for confirmation (unless `--auto`).
5. On confirmation: call `archigraph_save_finding(type="security_finding", ...)` to persist. In interactive mode, the user can dismiss, escalate, or edit the severity before submission.

Output: per-finding markdown pages at `~/.archigraph/docs/<group>/security/`.

## Output layout

```
~/.archigraph/docs/<group>/security/
  index.md                          # overview + severity summary table
  findings/
    <fingerprint>.md                # one page per confirmed finding
~/.archigraph/groups/<group>/archigraph-security-audit/
  phase1-findings.json              # raw Phase 1 output
  state.json                        # last-run SHA, finding counts
```

## archigraph MCP tool surface

- `archigraph_whoami`, `archigraph_find`, `archigraph_inspect`, `archigraph_expand`
- `archigraph_repairs` — check for residual edges on security-sensitive paths
- `archigraph_enrichments` — read enriched endpoint metadata
- `archigraph_save_finding` — persist confirmed security findings
- `archigraph_list_findings` — load prior findings to avoid duplicates

## Related

- `skills/archigraph-resolve/SKILL.md` — run before this; orphan endpoints are false negatives.
- `skills/archigraph-tech-docs/SKILL.md` — optional; improves Phase 2 LLM context.
- `internal/security/` — Phase 1 static analysis package (deterministic checks).
- ADR-0015 — residual repair; residuals on auth-sensitive paths are a security concern.

## Read next

After auditing, run the full consultant panel or review specific personas:
-> `/archigraph-consult` — panel of specialist personas; the security-auditor persona deduplicates against these findings rather than re-deriving them.
