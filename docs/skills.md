# Skills

grafel ships a family of Claude Code slash-command skills. Each skill owns one concern and is idempotent — safe to re-run after any graph change.

The canonical reference is:

**[`skills/README.md`](../skills/README.md)**

This page is a pointer and summary. Do not duplicate the skills/README.md content here.

---

## Install

Skills are installed automatically by `grafel install`. To refresh after an upgrade:

```sh
grafel install --skills    # refresh skills only
grafel doctor              # check which skills are installed and up-to-date
```

Skills land in `~/.claude/skills/` where Claude Code discovers them.

---

## Skill chain (summary)

```
/grafel-resolve            -- fix residual edges (run first)
    |
    +-- /grafel-graph-quality   -- benchmark graph health
    +-- /grafel-graph-enrich    -- populate Paths/Flows/Topology panels
    |
    +-- /grafel-tech-docs       -- engineer-facing module docs (13 passes)
    +-- /grafel-business-docs   -- PM-facing capabilities + journeys
    |
    +-- /grafel-security-audit  -- static + LLM security audit
    +-- /grafel-consult         -- 5-persona consultant panel
```

For the full chain diagram, hard vs. soft dependencies, all 15 skills, and the decision table ("which skill for my goal?"), see [skills/README.md](../skills/README.md).

---

## Minimum useful run (first time, ~30 min, ~$5-$20)

```
/grafel-resolve
/grafel-graph-enrich
```

Result: a queryable, dashboard-rich graph. No prose docs yet.

## Add documentation

```
/grafel-tech-docs
```

Produces per-module READMEs, API reference, cross-cutting concerns, and group synthesis. Runs in 25 min to 4 h depending on repo size.
