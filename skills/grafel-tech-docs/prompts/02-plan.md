# Pass 2 — Plan

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_expand`, `grafel_stats`,
`grafel_clusters`, `grafel_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_whoami` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

Convert the raw community list from Pass 1 into a documentation plan. The plan is the contract that Passes 3-6 execute against.

## Staging run

**FIRST action in this pass** (after `grafel_whoami`): call `grafel_docgen_start_run` to create a staging run for this group. Capture `run_id` and `staging_path` from the response and carry them through every subsequent write in this pass and all downstream passes.

```
grafel_docgen_start_run(group="<group>")
# response: { "run_id": "<id>", "staging_path": "<project>/.grafel/staging/<id>/" }
```

All doc files written in Passes 2–19 MUST target `<staging_path>/<relative-path>` rather than `~/.grafel/docs/<group>/`. The daemon promotes the staging directory to canonical at the end of Pass 20 via `grafel_docgen_promote`. Do NOT write to `~/.grafel/docs/` directly.

Pass the `run_id` and `staging_path` to every downstream writer subagent and the orchestrator.

## Inputs

- `~/.grafel/groups/<group>/domain.md`
- `~/.grafel/groups/<group>/inventory.json`

## Procedure

### Step 1 — Group communities into modules

A Louvain community from `grafel_clusters` is a graph cluster, not necessarily a "module" a human would want documented. Merge or split as needed:

- Merge two communities if they share more than 30% of their bridge-doc nodes or if their top-entity names share a clear prefix (e.g., `users.views`, `users.serializers` -> module `users`).
- Split a community if it contains entities from two unrelated layers (e.g., HTTP handlers + DB migrations); rare, but the convention file for the stack tells you when to expect it.

**Prefer real graph communities over directory fallback.** Call `grafel_clusters(repo_filter=["<r>"])` first. As of grafel #1620 communities persist through the daemon load path, so a non-empty cluster list is the norm. Only fall back to directory-derived modules when `grafel_clusters` genuinely returns `[]` for a repo.

#### Step 1b — Volume control (fragmentation guard)

This step fixes the audit finding where a single-app backend exploded into 122 directory-derived modules with empty `flows/index.md` stub triplets. Apply it whether modules came from communities or directory fallback, but it is **mandatory** on the directory-fallback path:

- **Merge thin modules.** A module with fewer than `min_module_entities` (default **8**) in-module entities is too thin to stand alone. Merge it into its nearest sibling — same parent directory, or the module whose top entities share its prefix. Only keep a sub-`min_module_entities` module standalone if it is a genuinely distinct public surface (e.g. a small but externally-consumed API module); note the exception in the plan entry.
- **Cap module count relative to size.** As a sanity check, target roughly one module per ~1.5–3k LOC. If the candidate module count exceeds `loc / 1500`, you are over-fragmenting — merge more aggressively. Record the final count and the merge decisions in `plan.json` under `volume_control`.
- **Never schedule empty stub pages.** A module gets a `flows.md` / `api.md` page in its plan entry ONLY if it has real content for it: `flows.md` only if the module owns ≥1 process flow or dynamic edge; `api.md` only if it is the primary owner of a public API. A module page (`README.md`) is always scheduled, but `flows`/`api` are conditional. Set `"pages": ["readme"]` (or add `"flows"`, `"api"`) per module so Pass 4 never emits an empty `flows/index.md`.
- **Index pages only when non-empty.** Only schedule a `modules/README.md` / section index if the section will have ≥1 child page. Pass 8 link-hygiene forbids linking to a directory whose index was never generated.

### Step 2 — Name modules

Each module gets a kebab-case slug used as a directory name under `docs/modules/`. Pull the slug from the dominant package/import path when one exists; otherwise, pick the most central entity's parent.

### Step 3 — Estimate writer cost per module

For each module, estimate:

- **Token budget** for the writer subagent's context: count of in-module entities + count of in-module edges, multiplied by a small constant per entity (start at 40 tokens). Cap at 8000 per module; if larger, split.
- **Source-snippet budget**: how many `grafel_get_source` calls the writer is allowed. Default 10; raise for modules with many small functions.

### Step 4 — Produce the plan file

Write `~/.grafel/groups/<group>/plan.json` (this is group metadata, NOT a doc file — write it to the groups directory directly, not to staging):

```json
{
  "group": "<group>",
  "run_id": "<run_id from grafel_docgen_start_run>",
  "staging_path": "<staging_path from grafel_docgen_start_run>",
  "tiers": ["technical", "business"],
  "primary_repo": "<slug>",
  "volume_control": {
    "min_module_entities": 8,
    "modules_before_merge": 122,
    "modules_after_merge": 46,
    "module_source": "communities",
    "merge_notes": ["merged sync/import/report dir-stubs into data-pipeline"]
  },
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
          "pages": ["readme", "flows"],
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
    "8_cross_link": { "candidates_to_review": 0 },
    "business": {
      "enabled": true,
      "primary_repo": "<slug>",
      "capabilities_estimate": 10,
      "journeys_estimate": 5
    }
  }
}
```

The `tiers` field records which documentation tiers the user selected (see
SKILL.md § Documentation tiers). `business` passes (15–19) are only scheduled if
`"business"` is in `tiers`; technical passes (3–8, 10–14) only if `"technical"`
is in `tiers`. `primary_repo` is retained for backwards compatibility but is
no longer used as a write location: business docs are always written to the
single group-level directory `~/.grafel/docs/<group>/business/` (#1624).
Treat `primary_repo` as a hint for which repo to lead with in cross-cutting
prose (default: the repo with the most entities — usually the
backend/service).

### Step 5 — Show the plan to the user

Print a compact human summary of the plan and ask:

> Proceed with this plan? You can edit `plan.json` directly or tell me what to change.

Wait for confirmation before handing back to the orchestrator. The orchestrator will not start Pass 3 without the user's explicit go-ahead.

## Notes

- If a module has `depends_on`, Pass 4 schedules it after its dependencies. Use this when one module's flow page must reference another module's API page.
- Modules whose `token_budget` exceeds 8000 must be split before the plan is finalized; the plan file is rejected by the orchestrator otherwise.

---

**[pass-02 telemetry]** Print at end of this pass:
```
[pass-02] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
