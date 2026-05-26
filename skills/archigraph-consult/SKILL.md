---
name: archigraph-consult
description: Run a panel of specialist personas against the indexed group and its generated docs. Each persona (architect, security-auditor, business-analyst, performance-reviewer, refactor-critic, api-designer, data-engineer, qa-reviewer) is a Claude Code subagent that reads module docs and produces a focused report. Findings are persisted as graph entities. The session is resumable.
when-to-use: User asks to "get a second opinion", "run the consultant panel", "have the architect review this", "get a business analyst view", "review my API design", "check test coverage", or invokes /archigraph-consult explicitly. Hard-depends on /archigraph-tech-docs having been run. Soft-depends on /archigraph-business-docs and /archigraph-security-audit.
---

# archigraph-consult

Run a panel of specialist personas against the indexed group. Each persona reads the generated tech docs (and optionally business docs and security findings) and produces a focused report from their perspective.

This is **one skill** that fans out to per-persona subagents. Personas are agent definition files — markdown files that the user's coding agent (Claude Code, Windsurf, Cursor) interprets at invocation time. They are **not CLI commands, not daemon processes, and not web-UI features**. See `docs/architecture/personas.md` for the full architectural contract.

Adding a new persona is as simple as dropping a `.md` file into `skills/archigraph-consult/personas/` (shipped) or `~/.claude/agents/archigraph-<name>.md` (user-defined). You do not need a new skill for each persona.

## Hard and soft dependencies

| Dependency | Type | Why |
|---|---|---|
| `/archigraph-resolve` | Hard | Orphan edges make impact-radius analysis meaningless |
| `/archigraph-tech-docs` | Hard | Personas read module markdown; without it they have only the graph — too narrow for useful findings |
| `/archigraph-business-docs` | Soft | Business-analyst persona fidelity improves; not required |
| `/archigraph-security-audit` | Soft | Security-auditor persona deduplicates against static findings rather than re-deriving them |

If tech docs are missing, the skill aborts:
> Tech docs not found at `~/.archigraph/docs/<group>/`. Run `/archigraph-tech-docs` first, then re-invoke `/archigraph-consult`.

## CRITICAL TOOL DISCIPLINE

Use archigraph MCP tools for ALL graph navigation. Pre-flight: call `archigraph_whoami` first.

## When to use this skill

- "Get an architect's view of this codebase."
- "Run the consultant panel."
- "Have the security auditor review the docs."
- "What would a performance reviewer say?"
- "Review the API design consistency."
- "What's the test coverage situation?"
- "Check the data layer quality."
- `/archigraph-consult` (slash command).

**Flags:**
- `--persona <name>` — run only one persona (e.g. `--persona architect`).
- `--all` — run all available personas (default when no `--persona` is specified).
- `--resume <session-id>` — resume an interrupted session.
- `--dry-run` — list available personas without running any.

## Shipped personas

Eight personas ship with the skill as files in `skills/archigraph-consult/personas/`. Users can add their own under `~/.claude/agents/archigraph-<persona>.md`.

| Persona | File | Focus | Status |
|---|---|---|---|
| architect | `personas/architect.md` | Module layering, coupling, cyclic deps, god modules, ADR opportunities | Tier A |
| security-auditor | `personas/security-auditor.md` | Auth gaps, PII exposure, injection risks, attack surface; deduplicates with `/archigraph-security-audit` | Tier A |
| business-analyst | `personas/business-analyst.md` | Capability coverage, feature gaps, business rule completeness, user-journey gaps | Tier A |
| performance-reviewer | `personas/performance-reviewer.md` | Hot paths, N+1 queries, synchronous blocking, unbounded queries, caching opportunities | Tier A |
| refactor-critic | `personas/refactor-critic.md` | Complexity hotspots, duplication, dead code, over-indirection, tech-debt surface | Tier A |
| api-designer | `personas/api-designer.md` | Endpoint naming, REST/RPC convention consistency, versioning, contract coverage, error-shape uniformity | Tier B |
| data-engineer | `personas/data-engineer.md` | Schema quality, migration hygiene, ORM query patterns, index candidates, FK integrity | Tier B |
| qa-reviewer | `personas/qa-reviewer.md` | TESTS-edge coverage by module, untested critical paths, test-type distribution, fixture hygiene | Tier B |

### Deferred personas (not shipped in this release)

The following personas are documented in `docs/architecture/personas.md` Section 5 but deferred:

| Persona | Deferral reason |
|---|---|
| solutions-architect | Needs validated `archigraph_cross_links` coverage and cross-repo topology tooling |
| devops-reviewer | IaC analysis outside archigraph's graph scope; requires separate static-analysis integration |
| compliance-officer | High false-positive rate without semantic data classification |
| dx-engineer | Low graph signal; metrics definition work needed first |

## Cross-persona coordination

Personas that overlap on the same entities should cite the **same entity IDs** in their findings so the editor synthesis pass can cross-reference them:

- `performance-reviewer` and `data-engineer` both flag N+1 patterns — cite the same loop → DB entity path.
- `security-auditor` and `data-engineer` both flag raw query risks — cite the same raw query entity ID.
- `refactor-critic` and `qa-reviewer` both flag high-degree untested entities — cite the same entity IDs.

## Procedure

### Pre-flight
1. Call `archigraph_whoami` — confirm group.
2. Check tech docs exist: `~/.archigraph/docs/<group>/` must contain at least one `modules/` directory. If not: abort with the message above.
3. Load available personas: scan `skills/archigraph-consult/personas/*.md` + `~/.claude/agents/archigraph-*.md`.
4. If `--dry-run`: list personas and exit.
5. Create session: write `~/.archigraph/consultations/<session-id>/session.json` with session metadata.

### Fan-out
For each selected persona (sequentially to avoid context contamination; parallel is fine if the agent host supports isolated subagents):

1. Load the persona's `.md` file.
2. Spawn a subagent with the persona's instructions + the group's tech docs path as context.
3. The subagent produces a Markdown report and a JSON findings list.
4. Write the report to `~/.archigraph/consultations/<session-id>/<persona-name>.md`.
5. Write findings to `~/.archigraph/consultations/<session-id>/<persona-name>-findings.json`.

### Editor synthesis
After all personas complete, an "editor" pass synthesises across reports:
- Deduplicates findings that multiple personas raised (using entity ID cross-references).
- Ranks findings by cross-persona agreement (findings raised by 3+ personas = high priority).
- Ranks by severity × confidence × blast_radius for findings not corroborated by multiple personas.
- Writes `~/.archigraph/consultations/<session-id>/synthesis.md`.

### Graph materialisation
For each deduplicated finding with `confidence >= 0.7`:
```
archigraph_save_finding(
  type="consultant_finding",
  question="<persona>: <finding_title>",
  answer="<explanation>",
  entity_id="<entity_id if applicable>"
)
```

### Session summary
Print:
> Consultation `<session-id>` complete. **N** personas ran, **F** findings (P deduplicated). Reports at `~/.archigraph/consultations/<session-id>/`.

## Output layout

```
~/.archigraph/consultations/<session-id>/
  session.json               # metadata: group, personas run, timestamps
  <persona-name>.md          # per-persona report
  <persona-name>-findings.json
  synthesis.md               # editor pass: deduplicated, ranked findings
```

## archigraph MCP tool surface

All personas use: `archigraph_whoami`, `archigraph_find`, `archigraph_inspect`, `archigraph_expand`, `archigraph_traces`, `archigraph_clusters`, `archigraph_stats`

Skill-level (post-persona): `archigraph_save_finding`, `archigraph_list_findings`

Cross-repo (where available): `archigraph_cross_links`

## Architecture reference

`docs/architecture/personas.md` — canonical contract covering persona shape, cross-platform delivery, orchestration flow, full persona catalog, anti-patterns, and phasing.

## Related

- `skills/archigraph-tech-docs/SKILL.md` — hard dependency.
- `skills/archigraph-business-docs/SKILL.md` — soft dependency.
- `skills/archigraph-security-audit/SKILL.md` — soft dependency; security-auditor persona deduplicates.
- `~/.claude/agents/` — user-defined custom personas go here.

## Read next

This is the final skill in the main analysis chain. After consulting:
→ `/archigraph-help` — overview of the full skill family and suggested entry points.
