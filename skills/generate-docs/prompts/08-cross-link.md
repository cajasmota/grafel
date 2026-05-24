# Pass 8 — Cross-link validation

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

---


Two responsibilities:

1. Validate that every relative link inside the generated docs resolves.
2. Walk every pending cross-repo link candidate and decide accept/reject with a rationale.

## Step 1 — Static link check

Apply `snippets/link-hygiene.md` and `snippets/anchor-contract.md` — they are the
source of truth for what a valid link/anchor is. This pass enforces them across
the whole tree (technical + business). For each generated markdown file, parse
every `[text](path)` and confirm:

- The path resolves to an existing file under a `docs/` tree (relative paths) or
  a known anchor (`#section-slug`).
- **No link points into a source-code directory** (`src/`, `core/`,
  `dockerfile`, etc.). Those must be backticked paths, not links — rewrite any
  that slipped through (link-hygiene rule 2).
- **No bare-directory link** (`](modules/)`, `](reference/)`) without a real
  index file target. Either repoint to a concrete page or to a generated
  `<dir>/README.md` that exists; if neither exists, drop the link and leave the
  text in backticks (link-hygiene rule 3).
- **Relative paths use the filesystem dirname, prose uses the slug.** The
  registry slug (`upvate-core`) and the on-disk dir (`upvate_core`) can differ.
  A link path with the slug where the directory uses an underscore is the
  `core-mobile → ../../upvate-core/docs/` 404 from the audit. Resolve the real
  dirname from `inventory.json` `repo.path` and rewrite (link-hygiene rule 4).
- The anchor matches the heading slug exactly per `snippets/anchor-contract.md`
  slugification. Additionally verify each file's declared `anchors:` frontmatter
  list: every declared anchor MUST have a matching heading in that same file
  (the 17-mismatch bug). If a file declares anchors with no matching heading,
  the writer built the list by hand — re-derive `anchors:` from the actual
  headings (anchor-contract procedure) and fix in place.
- **Don't keep links to optional pages that were never generated**
  (`how-to/local-dev.md`, a missing `reference/` index): drop them
  (link-hygiene rule 6).

Broken links are auto-fixed where the target is unambiguous; otherwise log them
and report at the end. The report's "Broken intra-doc links" section must be
empty for the run to be considered clean — every broken link is either repaired
or downgraded to a backticked identifier.

## Step 2 — Cross-repo link candidates

```
archigraph_cross_links(action=list, limit=200)
```

For each candidate:

```
archigraph_inspect(label_or_id="<from>")
archigraph_inspect(label_or_id="<to>")
archigraph_trace(source="<from>", target="<to>")
```

Decide:

- **Accept** if the connection is real and intended. Use the convention file's `cross_repo_signals` section to check whether the channel/method is one we trust by default.
- **Reject** if the candidate is a coincidental name collision or a stale hint (e.g., a doc that named an entity that no longer exists).
- **Override target** if the candidate is real but pointed at the wrong specific node — pass `override_target=<correct id>` when resolving.

Resolve each:

```
archigraph_cross_links(
  action="accept" | "reject",
  candidate_id="<id>",
  reason="<short explanation>",
  override_target="<optional>",
)
```

Record every decision in `~/.archigraph/groups/<group>/docs/cross-links.md` so a human can audit later.

## Step 3 — Enrichment loop

Anything that blocked a doc page in earlier passes shows up in:

```
archigraph_enrichments(action=list, limit=100)
```

For each:

- If you can answer it from the docs you just wrote, call `archigraph_enrichments(action=submit, candidate_id=..., value=..., confidence=...)`.
- If you cannot, call `archigraph_enrichments(action=reject, candidate_id=..., reason=...)`.
- If a human must decide, leave it alone and list it in the cross-link report under "Human-required enrichment".

## Step 4 — Report

Write `~/.archigraph/groups/<group>/docs/cross-links.md` with sections:

```markdown
# Cross-link review

## Accepted (N)
- `<from>` → `<to>` via <method>: <reason>

## Rejected (N)
- `<from>` → `<to>`: <reason>

## Overridden targets (N)
- `<from>` → original `<to>` replaced with `<new target>`: <reason>

## Broken intra-doc links
- <file>:<line> — <broken target> — <action taken>

## Human-required enrichment
- candidate `<id>` (`<kind>`): <description>
```

Hand back to the orchestrator. The orchestrator now decides whether to run Pass 10 (pattern convergence), which runs only if Pass 4 emitted pattern candidates.

---

**[pass-08 telemetry]** Print at end of this pass:
```
[pass-08] archigraph MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
