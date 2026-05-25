# Pass 1 — Inventory

---

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use archigraph MCP tools:
`archigraph_inspect`, `archigraph_find`, `archigraph_expand`, `archigraph_stats`,
`archigraph_clusters`, `archigraph_whoami`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `archigraph_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `archigraph_whoami` before doing anything else in this pass. If it errors:
ABORT with: "archigraph MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."

## CRITICAL STORAGE DISCIPLINE
===========================
All generated documentation MUST be written under:
  `~/.archigraph/docs/<group>/...`

Determine `<group>` via the `archigraph_whoami` MCP call (the Pre-flight assertion
above). Pass it through every subsequent file write as `${OUTPUT_ROOT}`.

You are STRICTLY FORBIDDEN from writing documentation files into:
- The source repo's working tree (anywhere under `<repo>/docs/`, `<repo>/doc/`, etc.)
- The CWD unless CWD is already inside `~/.archigraph/docs/<group>/`
- Any path that is a git working directory

If you find yourself about to write to a repo path, STOP. The skill assumes
the archigraph-owned store. Writing elsewhere breaks the storage contract
and pollutes the user's source repo.

The daemon dashboard reads from `~/.archigraph/docs/<group>/` -- any output
written elsewhere is invisible to it.

### Pre-flight storage assertion -- SECOND action in this pass

Compute and verify the output root immediately after the `archigraph_whoami` call:

```bash
OUTPUT_ROOT="$HOME/.archigraph/docs/<group>/"   # substitute <group> from whoami
mkdir -p "$OUTPUT_ROOT"
echo "OUTPUT_ROOT=$OUTPUT_ROOT"
```

All file writes in this pass MUST use `${OUTPUT_ROOT}<relative-path>`. Never write to any
other location. If `mkdir -p` fails, ABORT: "Cannot create output directory at $OUTPUT_ROOT."
## CRITICAL OUTPUT DISCIPLINE
==========================
The generate-docs skill produces markdown files in the canonical store
at `~/.archigraph/docs/<group>/`. It does NOT produce:
- VitePress / Docusaurus / Sphinx / mkdocs scaffolding
- `package.json` or any build manifests for static site generators
- Any non-markdown asset that wraps the docs for publishing
- `.gitignore` entries

Publishing is downstream — handled by the archigraph dashboard or
external tooling. If you find yourself about to write a `config.ts`,
`package.json`, `mkdocs.yml`, `.vitepress/config.ts`, or any build
manifest, STOP. The skill's job is content, not infrastructure.




---


Discover what is actually in each repo by querying archigraph. Do not read source files directly. The graph is the source of truth for what entities exist; the writer passes will read source via `archigraph_get_source` when they need verbatim snippets.

## Inputs

- `~/.archigraph/groups/<group>/domain.md` from Pass 0.
- An archigraph MCP session.

## Procedure

### Step 1 — Confirm group

Call `archigraph_whoami`. Verify the resolved group matches what the user expected. If it does not, stop and ask the user to set `cwd` or pass `group` explicitly.

### Step 2 — Corpus metrics

For each repo `<r>` in the group:

```
archigraph_stats(repo_filter=["<r>"])
```

Record: total nodes, total edges, top entity kinds, top edge kinds, communities count, bridge-doc count.

### Step 3 — Community seeds

For each repo:

```
archigraph_clusters(repo_filter=["<r>"])
```

A community is a candidate "module" for Pass 2 to plan around. Capture community id, size, and the top-5 entities (by centrality) in each. This is your raw module list before grouping.

### Step 4 — Cross-repo bridges

```
archigraph_cross_links(action=list, limit=50)
```

These are pending cross-repo edges. Note them — Pass 8 will resolve them, but writer subagents in Pass 4 should know they exist so they do not invent contradictory descriptions.

### Step 5 — Enrichment debt

For each repo:

```
archigraph_enrichments(action=list, repo_filter=["<r>"], limit=20)
```

If anything blocks accurate documentation (e.g., an unresolved env-var, a class with unknown base), flag it. Pass 4 writers will need to either route around it or prompt the user during the deep-dive.

### Step 6 — Recent activity (optional)

If the user said "regenerate after the recent refactor", call `archigraph_recent_activity(since=<timestamp>)` and tag those entities so Pass 2 prioritizes their modules.

## Output

Write `~/.archigraph/groups/<group>/inventory.json`:

```json
{
  "group": "<group>",
  "generated_at": "<RFC3339>",
  "repos": [
    {
      "repo": "<slug>",
      "stats": { "nodes": 0, "edges": 0, "kinds": {}, "edge_kinds": {} },
      "communities": [
        { "id": "c1", "size": 0, "top_nodes": ["`Foo`", "`Bar`"] }
      ],
      "enrichment_debt": [
        { "candidate_id": "...", "kind": "...", "blocking": true }
      ]
    }
  ],
  "link_candidates": [
    { "candidate_id": "...", "from": "...", "to": "...", "method": "..." }
  ]
}
```

When the file is written, hand control back to the orchestrator with its path. Do not move on to Pass 2 yourself.

---

**[pass-01 telemetry]** Print at end of this pass:
```
[pass-01] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
