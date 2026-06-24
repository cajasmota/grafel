# Pass 8 — Cross-link validation

---

## Staging path

Read `run_id` and `staging_path` from `~/.grafel/groups/<group>/plan.json` (written by Pass 2). All doc files produced by this pass MUST be written into `<staging_path>/<relative-path>` — NOT directly to `~/.grafel/docs/<group>/`. Wherever this prompt says `~/.grafel/docs/<group>/`, substitute `<staging_path>/`. The daemon promotes staging to canonical at the end of Pass 20.

## CRITICAL TOOL DISCIPLINE
========================
For ANY question about "what entities/files exist in this codebase", "who calls X",
"what does Y import", "what's in module Z", you MUST use grafel MCP tools:
`grafel_inspect`, `grafel_find`, `grafel_subgraph`, `grafel_orient (view=overview)`,
`grafel_orient (view=clusters)`, `grafel_orient (view=me)`, (full list in SKILL.md).

You are STRICTLY FORBIDDEN from using `find`/`ls`/`wc`/`grep` on the codebase for
entity discovery, or reading source files directly to enumerate APIs.

The MCP daemon has the resolved graph; trust it. Use Bash ONLY for reading specific
source line ranges that `grafel_get_source` returns, or writing output files.

If the MCP returns empty or seems wrong, file a side ticket and ABORT --
do NOT silently substitute grep results for graph queries.

### Pre-flight assertion -- FIRST action in this pass

Call `grafel_orient (view=me)` before doing anything else in this pass. If it errors:
ABORT with: "grafel MCP not configured for this directory. Run `/mcp` to fix, then re-invoke `/generate-docs`."


---

Two responsibilities:

1. Validate that every relative link inside the generated docs resolves.
2. Walk every pending cross-repo link candidate and decide accept/reject with a rationale.

## Step 1 — Static link check via MCP

Call `grafel_docgen (action=validate)` to lint frontmatter and cross-links across the
entire staging tree in a single structured response. Pass the `run_id` from
`plan.json`:

```
grafel_docgen(action="validate", run_id="<run_id>")
# response: { "frontmatter_errors": [...], "link_errors": [...], "ok": true|false }
```

Report every entry in `link_errors` to the user; these are the broken intra-doc
links. Broken links must be repaired (or downgraded to backticked identifiers)
before the run can be considered clean — the report's "Broken intra-doc links"
section must be empty.

The `grafel_docgen (action=validate)` tool applies the same rules as
`snippets/link-hygiene.md` and `snippets/anchor-contract.md` (which remain the
source-of-truth documentation for WHAT constitutes a valid link/anchor). The
tool enforces them across the whole staging tree including:

- Paths that resolve to existing files (relative links)
- No links into source-code directories (`src/`, `core/`, `dockerfile`, etc.)
- No bare-directory links without a real index target
- Relative paths using the filesystem dirname (not the registry slug)
- Anchor slugs matching actual headings; declared `anchors:` frontmatter matching headings
- No links to optional pages that were never generated

For any `link_error` the tool flags, auto-fix where the target is unambiguous;
otherwise log it and escalate to the user.

## Step 2 — Cross-repo link candidates

```
grafel_cross_links(action=list, limit=200)
```

For each candidate:

```
grafel_inspect(label_or_id="<from>")
grafel_inspect(label_or_id="<to>")
grafel_trace(source="<from>", target="<to>")
```

Decide:

- **Accept** if the connection is real and intended. Use the convention file's `cross_repo_signals` section to check whether the channel/method is one we trust by default.
- **Reject** if the candidate is a coincidental name collision or a stale hint (e.g., a doc that named an entity that no longer exists).
- **Override target** if the candidate is real but pointed at the wrong specific node — pass `override_target=<correct id>` when resolving.

Resolve each:

```
grafel_cross_links(
  action="accept" | "reject",
  candidate_id="<id>",
  reason="<short explanation>",
  override_target="<optional>",
)
```

Record every decision in `~/.grafel/groups/<group>/docs/cross-links.md` so a human can audit later.

## Step 3 — Enrichment loop

Anything that blocked a doc page in earlier passes shows up in:

```
grafel_docgen_apply(kind="enrichments", action=list, limit=100)
```

For each:

- If you can answer it from the docs you just wrote, call `grafel_docgen_apply(kind="enrichments", action=submit, candidate_id=..., value=..., confidence=...)`.
- If you cannot, call `grafel_docgen_apply(kind="enrichments", action=reject, candidate_id=..., reason=...)`.
- If a human must decide, leave it alone and list it in the cross-link report under "Human-required enrichment".

## Step 4 — Report

Write `~/.grafel/groups/<group>/docs/cross-links.md` with sections:

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
[pass-08] grafel MCP calls: X | Bash invocations: Y
```
If Y > 5 and X < 10: print warning "Likely fallback pattern detected -- investigate skill prompt."
