# Pass 8 — Cross-link validation

Two responsibilities:

1. Validate that every relative link inside the generated docs resolves.
2. Walk every pending cross-repo link candidate and decide accept/reject with a rationale.

## Step 1 — Static link check

For each generated markdown file, parse every `[text](path)` and confirm:

- The path resolves to an existing file (relative paths) or a known anchor (`#section-slug`).
- The anchor matches the heading slug exactly. The slugification rule: lowercase, spaces → hyphens, drop non-alphanumeric except `-`; your anchor check must match that.

Broken links are auto-fixed where the target is unambiguous; otherwise log them and report at the end.

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
