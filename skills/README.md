# grafel skills

A family of focused, independently invokable skills for working with grafel knowledge graphs. Each skill owns one concern and is idempotent — safe to re-run after any graph change.

> **New here?** Start with [`/grafel-help`](grafel-help/SKILL.md) for a complete orientation, or follow the chain below.

---

## Skill chain (canonical order)

```
┌─────────────────────────────────────────────────────────────────────┐
│  FOUNDATION — run these first                                        │
│                                                                      │
│  /grafel-resolve          Surface + resolve residual edges       │
│        │                                                             │
│        ├──(soft)──► /grafel-graph-quality   Health benchmark     │
│        └──(soft)──► /grafel-graph-enrich    Dashboard panels     │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  DOCUMENTATION — independent siblings                                │
│                                                                      │
│  /grafel-tech-docs        Engineer-facing module docs            │
│  /grafel-business-docs    PM-facing capabilities + journeys      │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  DERIVED VALUE — read from graph + docs                              │
│                                                                      │
│  /grafel-security-audit   Static + LLM security findings        │
│  /grafel-consult          5-persona consultant panel             │
└─────────────────────────────────────────────────────────────────────┘
```

**Hard dependency (→):** must complete before consumer starts.
**Soft dependency (soft →):** improves quality; not required.
`/grafel-tech-docs` is a hard dependency for `/grafel-consult`.
`/grafel-business-docs` does NOT hard-depend on `/grafel-tech-docs` — graph-only fallback is built in.

---

## All skills

### Core chain

| Skill | Directory | Purpose |
|-------|-----------|---------|
| [`/grafel-resolve`](grafel-resolve/SKILL.md) | `skills/grafel-resolve/` | Resolve residual edges from static analysis — runtime dispatch, dynamic URLs, ambiguous bindings. Absorbs generate-docs passes 1a + 1b. |
| [`/grafel-graph-quality`](grafel-graph-quality/SKILL.md) | `skills/grafel-graph-quality/` | MCP vs grep+read benchmark. Confirms graph health before spending tokens. Supports `--since <sha>` delta mode for CI. |
| [`/grafel-graph-enrich`](grafel-graph-enrich/SKILL.md) | `skills/grafel-graph-enrich/` | Emit YAML frontmatter for `http_endpoint`, `process_flow`, `message_topic` entities. Makes Paths/Flows/Topology panels light up. |
| [`/grafel-tech-docs`](grafel-tech-docs/SKILL.md) | `skills/grafel-tech-docs/` | 13-pass technical documentation pipeline: per-module READMEs, API reference, cross-cutting concerns, group synthesis, patterns. |
| [`/grafel-business-docs`](grafel-business-docs/SKILL.md) | `skills/grafel-business-docs/` | PM-facing docs synthesised across the group: capabilities, glossary, user journeys, business rules. No hard dependency on tech docs. |
| [`/grafel-security-audit`](grafel-security-audit/SKILL.md) | `skills/grafel-security-audit/` | Two-phase security audit: deterministic static checks (Phase 1, free) + LLM semantic confirmation (Phase 2, interactive). |
| [`/grafel-consult`](grafel-consult/SKILL.md) | `skills/grafel-consult/` | 5-persona consultant panel: architect, security auditor, business analyst, performance reviewer, refactor critic. Requires tech docs. |

### Utilities

| Skill | Directory | Purpose |
|-------|-----------|---------|
| [`/grafel-patterns-discover`](grafel-patterns-discover/SKILL.md) | `skills/grafel-patterns-discover/` | Discover recurring structural patterns across the group. Standalone. |
| [`/grafel-patterns-sync`](grafel-patterns-sync/SKILL.md) | `skills/grafel-patterns-sync/` | Bidirectional sync of pattern markers with CLAUDE.md files. |
| [`/grafel-aware-review`](grafel-aware-review/SKILL.md) | `skills/grafel-aware-review/` | PR-review-time skill using the graph to add architectural context. |
| [`/grafel-test-page`](grafel-test-page/SKILL.md) | `skills/grafel-test-page/` | Single-entity smoke test of the LLM docgen emit→fill→apply loop. Debugging tool. |
| [`/extend-convention`](extend-convention/SKILL.md) | `skills/extend-convention/` | Generate a stack convention file for a new language/framework. |
| [`/using-grafel`](using-grafel/SKILL.md) | `skills/using-grafel/` | Day-to-day orientation: how to query, navigate, and maintain the graph. |
| [`/grafel-help`](grafel-help/SKILL.md) | `skills/grafel-help/` | Full skill family reference: chains, decision table, install commands. Start here if you're new. |

---

## Install

Skills ship with the grafel binary:

```bash
# Install all skills (first time or after upgrade):
grafel install

# Dev mode (symlinks instead of copies, for editing skills in-place):
grafel install --dev

# Check which skills are installed and up-to-date:
grafel doctor
```

Skills land in `~/.claude/skills/` where Claude Code can discover them.

---

## Adding a persona to `/grafel-consult`

Drop a Markdown file at `~/.claude/agents/grafel-<persona-name>.md` following the pattern in [`skills/grafel-consult/personas/`](grafel-consult/personas/). The consult skill discovers it automatically.

---

## Retired skills

| Old skill | Replaced by |
|-----------|-------------|
| `/generate-docs` | `/grafel-tech-docs` + `/grafel-business-docs` + `/grafel-graph-enrich` |
| `/grafel-repair` | `/grafel-resolve` |
| `/grafel-quality-check` | `/grafel-graph-quality` |
