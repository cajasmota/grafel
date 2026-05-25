# Pass 11 — Pattern cross-link (Phase 5 of ADR-0018)

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


For every pattern approved in Pass 10 (or refined in this run), populate its `documentation_url` field so the graph holds a forward pointer to the markdown emitted in Pass 12.

## Procedure

For each approved pattern `p`:

1. Compute the canonical doc URL:

   ```
   docs/patterns/<category>/<pattern-id>.md
   ```

   The `archigraph_patterns(action=get, id=<id>)` response includes both `category` and `id`; the relative path is fully determined from the pattern itself. The renderer in `internal/agentpatterns/docs.go` exposes `DocPathFor(p)` so the value is consistent on both ends.

2. Refine the pattern with the URL:

   ```
   archigraph_patterns(action=refine, pattern_id=<id>, changes={ "set_documentation_url": "<computed url>" })
   ```

   Refinement is confidence-neutral, so this call does not perturb the lifecycle.

3. Cross-link from the prose. When Pass 4 (cluster) or Pass 6 (cross-cutting) prose touches an entity that has an incoming `CREATED_BY` edge from a Pattern, add an inline link at first mention:

   ```
   When adding a handler, follow the [endpoint pattern](../patterns/code/<id>.md).
   ```

   This is the doc-as-bridge convention from ADR-0007: the prose carries the link, the graph carries the relationship; doc generators on both sides stay loosely coupled.

## Constraints

- DO NOT write `documentation_url` for `is_candidate=true` patterns. Their docs are not generated.
- DO NOT delete an existing `documentation_url` when re-running — refinement only overwrites it with the new computed URL.
- DO NOT chase `SUPERSEDES` edges here. Superseded patterns keep their old documentation_url until the user runs `archigraph patterns delete` or v1.1's history surface lights up.

---

**[pass-11 telemetry]** Print at end of this pass:
```
[pass-11] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
