# Pass 2 — Plan

Convert the raw community list from Pass 1 into a documentation plan. The plan is the contract that Passes 3-6 execute against.

## Inputs

- `~/.archigraph/groups/<group>/domain.md`
- `~/.archigraph/groups/<group>/inventory.json`

## Procedure

### Step 1 — Group communities into modules

A Louvain community from `archigraph_clusters` is a graph cluster, not necessarily a "module" a human would want documented. Merge or split as needed:

- Merge two communities if they share more than 30% of their bridge-doc nodes or if their top-entity names share a clear prefix (e.g., `users.views`, `users.serializers` -> module `users`).
- Split a community if it contains entities from two unrelated layers (e.g., HTTP handlers + DB migrations); rare, but the convention file for the stack tells you when to expect it.

### Step 2 — Name modules

Each module gets a kebab-case slug used as a directory name under `docs/modules/`. Pull the slug from the dominant package/import path when one exists; otherwise, pick the most central entity's parent.

### Step 3 — Estimate writer cost per module

For each module, estimate:

- **Token budget** for the writer subagent's context: count of in-module entities + count of in-module edges, multiplied by a small constant per entity (start at 40 tokens). Cap at 8000 per module; if larger, split.
- **Source-snippet budget**: how many `archigraph_get_source` calls the writer is allowed. Default 10; raise for modules with many small functions.

### Step 4 — Produce the plan file

Write `~/.archigraph/groups/<group>/plan.json`:

```json
{
  "group": "<group>",
  "passes": {
    "3_overview": { "repos": ["<slug>", "..."] },
    "4_cluster": {
      "modules": [
        {
          "repo": "<slug>",
          "module": "<module-slug>",
          "title": "<Human title>",
          "convention": "django.md",
          "communities": ["c1", "c4"],
          "token_budget": 6500,
          "source_snippets": 10,
          "depends_on": []
        }
      ]
    },
    "5_reference": {
      "repos": [
        { "repo": "<slug>", "sections": ["api", "config", "deployment", "scripts", "dependencies"] }
      ]
    },
    "6_cross_cutting": {
      "topics": ["auth", "logging", "errors", "observability"]
    },
    "7_synthesis": { "scope": "group" },
    "8_cross_link": { "candidates_to_review": 0 }
  }
}
```

### Step 5 — Show the plan to the user

Print a compact human summary of the plan and ask:

> Proceed with this plan? You can edit `plan.json` directly or tell me what to change.

Wait for confirmation before handing back to the orchestrator. The orchestrator will not start Pass 3 without the user's explicit go-ahead.

## Notes

- If a module has `depends_on`, Pass 4 schedules it after its dependencies. Use this when one module's flow page must reference another module's API page.
- Modules whose `token_budget` exceeds 8000 must be split before the plan is finalized; the plan file is rejected by the orchestrator otherwise.
