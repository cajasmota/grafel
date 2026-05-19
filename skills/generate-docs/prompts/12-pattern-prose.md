# Pass 12 — Pattern prose generation (Phase 6 of ADR-0018)

Emit one markdown file per approved pattern under `docs/patterns/<category>/<pattern-id>.md`. Re-runs of `/generate-docs` overwrite these files from the current pattern store, so refinements and new applies propagate automatically.

This pass uses the `internal/agentpatterns/docs.go` renderer (`RenderMarkdown`, `WriteMarkdown`) — call it via the daemon's pattern surface rather than reimplementing the markdown construction.

## Output structure

Every generated pattern doc follows the same shape. The renderer enforces it; do not deviate.

```markdown
# <Trigger natural-language, with backticked code identifiers per ADR-0007>

- **Status**: Active
- **Category**: <code | process | team | tooling | architecture>
- **Confidence**: <0.00-1.00> (<N> observations, last applied <YYYY-MM-DD>)

## When to use

<Re-wording of trigger.natural_language as a single paragraph; the renderer
applies the same auto-backtick pass as the title so code identifiers stay
quoted.>

## Recipe

1. <Step 1 with backticked code identifiers>
2. <Step 2>
...

## Exemplars

| Entity | File | Lines |
|---|---|---|
| `<entity_name>` | <file_path> | <start-end> |
...

## Anti-patterns

- **Don't**: <do_not text with backticked code>
  - **Reason**: <reason>

(Anti-patterns with `private=true` MUST be omitted. The renderer enforces
this; if you bypass the renderer you must enforce it yourself.)

## Related patterns

- [`<related trigger>`](../<category>/<other-id>.md) (via SUPERSEDES | CO_APPLIES_WITH | PREREQUISITE)
```

## Backtick convention (ADR-0007)

EVERY code identifier in a heading must be wrapped in backticks. The renderer's auto-backtick pass handles common cases (CamelCase ≥3 segments, dotted paths, function calls, `snake_case` ≥3 segments, `SCREAMING_SNAKE`); CI runs `internal/agentpatterns.CheckBacktickConventionDir` against `docs/patterns/` and fails the build on any heading that contains a code-looking identifier outside backticks.

If a heading legitimately contains a CamelCase word that is NOT a code identifier (e.g. a product name), include it inside backticks anyway — the slug-collision rule from ADR-0007 cares about the slug, not the semantic.

## Procedure

For each approved pattern `p` (every pattern with `is_candidate=false`) that was newly promoted in this run OR refined in this run:

1. Resolve exemplar entities to `ExemplarRef` tuples via `archigraph_describe` for entity-name + `archigraph_get_source` for file path + line range.
2. Resolve outgoing `SUPERSEDES` / `CO_APPLIES_WITH` / `PREREQUISITE` edges via `archigraph_related(entity_id=<pattern_id>, depth=1)`; convert hits into `RelatedPattern` entries.
3. Call `WriteMarkdown(<docs_root>, MarkdownInput{Pattern: p, ExemplarRefs: ..., RelatedPatterns: ...})`. The renderer:
   - Skips when `is_candidate=true` (returns empty markdown — caller is expected to no-op).
   - Strips private anti-patterns.
   - Auto-backticks code identifiers in headings + body.
   - Writes to `<docs_root>/<category>/<id>.md` atomically.
4. After writing, run `CheckBacktickConvention` on the produced markdown. If violations are reported, fail the pass with the exact heading line — do NOT silently re-write.

## Constraints

- DO NOT generate docs for `is_candidate=true` patterns. The renderer skips them; this is a hard invariant.
- DO NOT omit the `Status`, `Category`, `Confidence` front-matter block. Downstream tooling parses it.
- DO NOT inline private anti-patterns under any circumstance. Tests in `internal/agentpatterns/docs_test.go` enforce this.
- DO NOT skip Pass 11 — without `documentation_url`, the graph cannot link prose to pattern docs.
