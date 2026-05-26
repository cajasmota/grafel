# archigraph skills

A family of focused, independently invokable skills for working with archigraph knowledge graphs. Each skill owns one concern and is idempotent — safe to re-run after any graph change.

> **New here?** Start with [`/archigraph-help`](archigraph-help/SKILL.md) for a complete orientation, or follow the chain below.

---

## Skill chain (canonical order)

```
┌─────────────────────────────────────────────────────────────────────┐
│  FOUNDATION — run these first                                        │
│                                                                      │
│  /archigraph-resolve          Surface + resolve residual edges       │
│        │                                                             │
│        ├──(soft)──► /archigraph-graph-quality   Health benchmark     │
│        └──(soft)──► /archigraph-graph-enrich    Dashboard panels     │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  DOCUMENTATION — independent siblings                                │
│                                                                      │
│  /archigraph-tech-docs        Engineer-facing module docs            │
│  /archigraph-business-docs    PM-facing capabilities + journeys      │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  DERIVED VALUE — read from graph + docs                              │
│                                                                      │
│  /archigraph-security-audit   Static + LLM security findings        │
│  /archigraph-consult          5-persona consultant panel             │
└─────────────────────────────────────────────────────────────────────┘
```

**Hard dependency (→):** must complete before consumer starts.
**Soft dependency (soft →):** improves quality; not required.
`/archigraph-tech-docs` is a hard dependency for `/archigraph-consult`.
`/archigraph-business-docs` does NOT hard-depend on `/archigraph-tech-docs` — graph-only fallback is built in.

---

## All skills

### Core chain

| Skill | Directory | Purpose |
|-------|-----------|---------|
| [`/archigraph-resolve`](archigraph-resolve/SKILL.md) | `skills/archigraph-resolve/` | Resolve residual edges from static analysis — runtime dispatch, dynamic URLs, ambiguous bindings. Absorbs generate-docs passes 1a + 1b. |
| [`/archigraph-graph-quality`](archigraph-graph-quality/SKILL.md) | `skills/archigraph-graph-quality/` | MCP vs grep+read benchmark. Confirms graph health before spending tokens. Supports `--since <sha>` delta mode for CI. |
| [`/archigraph-graph-enrich`](archigraph-graph-enrich/SKILL.md) | `skills/archigraph-graph-enrich/` | Emit YAML frontmatter for `http_endpoint`, `process_flow`, `message_topic` entities. Makes Paths/Flows/Topology panels light up. |
| [`/archigraph-tech-docs`](archigraph-tech-docs/SKILL.md) | `skills/archigraph-tech-docs/` | 13-pass technical documentation pipeline: per-module READMEs, API reference, cross-cutting concerns, group synthesis, patterns. |
| [`/archigraph-business-docs`](archigraph-business-docs/SKILL.md) | `skills/archigraph-business-docs/` | PM-facing docs synthesised across the group: capabilities, glossary, user journeys, business rules. No hard dependency on tech docs. |
| [`/archigraph-security-audit`](archigraph-security-audit/SKILL.md) | `skills/archigraph-security-audit/` | Two-phase security audit: deterministic static checks (Phase 1, free) + LLM semantic confirmation (Phase 2, interactive). |
| [`/archigraph-consult`](archigraph-consult/SKILL.md) | `skills/archigraph-consult/` | 5-persona consultant panel: architect, security auditor, business analyst, performance reviewer, refactor critic. Requires tech docs. |

### Utilities

| Skill | Directory | Purpose |
|-------|-----------|---------|
| [`/archigraph-patterns-discover`](archigraph-patterns-discover/SKILL.md) | `skills/archigraph-patterns-discover/` | Discover recurring structural patterns across the group. Standalone. |
| [`/archigraph-patterns-sync`](archigraph-patterns-sync/SKILL.md) | `skills/archigraph-patterns-sync/` | Bidirectional sync of pattern markers with CLAUDE.md files. |
| [`/archigraph-aware-review`](archigraph-aware-review/SKILL.md) | `skills/archigraph-aware-review/` | PR-review-time skill using the graph to add architectural context. |
| [`/archigraph-test-page`](archigraph-test-page/SKILL.md) | `skills/archigraph-test-page/` | Single-entity smoke test of the LLM docgen emit→fill→apply loop. Debugging tool. |
| [`/extend-convention`](extend-convention/SKILL.md) | `skills/extend-convention/` | Generate a stack convention file for a new language/framework. |
| [`/using-archigraph`](using-archigraph/SKILL.md) | `skills/using-archigraph/` | Day-to-day orientation: how to query, navigate, and maintain the graph. |
| [`/archigraph-help`](archigraph-help/SKILL.md) | `skills/archigraph-help/` | Full skill family reference: chains, decision table, install commands. Start here if you're new. |

---

## Install

Skills ship with the archigraph binary:

```bash
# Install all skills (first time or after upgrade):
archigraph install

# Dev mode (symlinks instead of copies, for editing skills in-place):
archigraph install --dev

# Check which skills are installed and up-to-date:
archigraph doctor
```

Skills land in `~/.claude/skills/` where Claude Code can discover them.

---

## Adding a persona to `/archigraph-consult`

Drop a Markdown file at `~/.claude/agents/archigraph-<persona-name>.md` following the pattern in [`skills/archigraph-consult/personas/`](archigraph-consult/personas/). The consult skill discovers it automatically.

---

## Retired skills

| Old skill | Replaced by |
|-----------|-------------|
| `/generate-docs` | `/archigraph-tech-docs` + `/archigraph-business-docs` + `/archigraph-graph-enrich` |
| `/archigraph-repair` | `/archigraph-resolve` |
| `/archigraph-quality-check` | `/archigraph-graph-quality` |
