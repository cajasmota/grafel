---
name: grafel-help
description: Overview of the grafel skill family. Lists all skills with one-line purposes, shows the canonical execution chain, and suggests which skill to invoke first based on your goal. Does NOT run any analysis — purely informational.
when-to-use: User asks "what grafel skills are there", "how do I use grafel", "where do I start", "what order should I run the skills", "which skill should I use", or invokes /grafel-help explicitly. Also useful as a first step when returning to a group after time away.
---

# grafel-help

Overview of the grafel skill family. Use this to orient yourself or to suggest the right starting point to a new team member.

## Skill family

| Skill | One-line purpose |
|-------|-----------------|
| `/grafel-resolve` | Surface and resolve residual edges (runtime dispatch, dynamic URLs). Run first. |
| `/grafel-graph-quality` | Benchmark MCP vs grep+read on ~10–15 questions. Run to confirm graph health before spending tokens. |
| `/grafel-graph-enrich` | Emit YAML frontmatter for endpoints/flows/topics so the dashboard Paths, Flows, Topology panels display data. |
| `/grafel-tech-docs` | Generate per-module technical documentation for engineers. The big one — 13 passes, 25 min – 4 h. |
| `/grafel-business-docs` | Generate PM-facing capabilities, user journeys, business rules synthesised across the group. Independent of tech docs. |
| `/grafel-security-audit` | Two-phase security audit: static analysis (free) + LLM confirmation (interactive). |
| `/grafel-consult` | Panel of 5 specialist personas (architect, security auditor, business analyst, performance reviewer, refactor critic). Requires tech docs. |
| `/grafel-patterns-discover` | Discover recurring structural patterns across the group. Standalone. |
| `/grafel-patterns-sync` | Bidirectional sync of pattern markers with CLAUDE.md files. |
| `/grafel-aware-review` | PR-review-time skill that uses the graph to add context to code reviews. |
| `/grafel-test-page` | Single-entity smoke test of the LLM docgen loop. Debugging tool. |
| `/extend-convention` | Generate a stack convention file for a new language/framework. |
| `/using-grafel` | Orientation skill explaining how to use grafel day-to-day. |
| `/grafel-help` | This skill. |

## Canonical execution chains

### Minimum useful (first time on a new repo, ~30 min, ~$5–$20)
```
/grafel-resolve
/grafel-graph-quality     (optional but recommended)
/grafel-graph-enrich
```
Result: a queryable, dashboard-rich graph. No prose docs yet.

### Technical documentation (adds 25 min – 4 h)
```
/grafel-resolve
/grafel-graph-enrich      (optional)
/grafel-tech-docs
```

### Business documentation (independent, adds 25 min – 1 h)
```
/grafel-resolve
/grafel-business-docs
```
Does NOT require tech docs. Graph-only fallback is built in.

### Full pipeline (pre-release / pre-audit, 1–6 h)
```
/grafel-resolve
/grafel-graph-quality
/grafel-graph-enrich
/grafel-tech-docs
/grafel-business-docs
/grafel-security-audit
/grafel-consult
```

### Daily maintenance (after a commit, < 10 min)
```
/grafel-resolve --delta-only
/grafel-graph-enrich --delta-only
/grafel-tech-docs --delta-only
```

## Which skill should I start with?

| Your goal | Start here |
|-----------|-----------|
| "Is the graph trustworthy?" | `/grafel-graph-quality` |
| "Fix dangling edges / residuals" | `/grafel-resolve` |
| "Make dashboard panels show data" | `/grafel-graph-enrich` |
| "Document the code for engineers" | `/grafel-tech-docs` |
| "Document the product for PMs" | `/grafel-business-docs` |
| "Find security issues" | `/grafel-security-audit` |
| "Get an expert second opinion" | `/grafel-consult` |
| "Find recurring patterns" | `/grafel-patterns-discover` |
| "Review a PR with graph context" | `/grafel-aware-review` |
| "Set up a new stack" | `/extend-convention` |
| "Just got started" | `/using-grafel` |

## Dependency quick-reference

```
grafel-resolve
  └─(soft)─> grafel-graph-quality
  └─(soft)─> grafel-graph-enrich
  ├─(hard)─> grafel-tech-docs
  │             └─(hard)─> grafel-consult
  ├─(hard)─> grafel-business-docs
  │             └─(soft)─> grafel-consult
  └─(hard)─> grafel-security-audit
                └─(soft)─> grafel-consult
```

Legend: `hard` = must complete before consumer starts. `soft` = improves quality but not required.

## Install

All skills ship with the grafel binary and are installed by `grafel install` or `grafel install --dev`. To check which skills are installed and up-to-date:

```bash
grafel doctor
```

To refresh all skills after an grafel upgrade:

```bash
grafel install --skills
```
