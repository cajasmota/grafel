# Pass 12 — Pattern prose generation (Phase 6 of ADR-0018)

Emit one markdown file per approved pattern under `docs/patterns/<category>/<pattern-id>.md`. Re-runs of `/generate-docs` overwrite these files from the current pattern store, so refinements and new applies propagate automatically.

> **Pass 3a hook active.** Before writing the section that names an exemplar entity, run the generation-time repair hook from `prompts/03a-generation-time-repair.md`. This prevents pattern exemplars from referencing dangling targets and implements the "patterns → repair" cross-link from #732.

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

## Conflicts

> This pattern conflicts with [`<other trigger>`](../<category>/<other-id>.md): <reason why they cannot both apply>.

*(Omit this section entirely if no `CONFLICTS_WITH` edges exist for this pattern.)*
```

## Backtick convention (ADR-0007)

EVERY code identifier in a heading must be wrapped in backticks. The renderer's auto-backtick pass handles common cases (CamelCase ≥3 segments, dotted paths, function calls, `snake_case` ≥3 segments, `SCREAMING_SNAKE`); CI runs `internal/agentpatterns.CheckBacktickConventionDir` against `docs/patterns/` and fails the build on any heading that contains a code-looking identifier outside backticks.

If a heading legitimately contains a CamelCase word that is NOT a code identifier (e.g. a product name), include it inside backticks anyway — the slug-collision rule from ADR-0007 cares about the slug, not the semantic.

## Procedure

For each approved pattern `p` (every pattern with `is_candidate=false`) that was newly promoted in this run OR refined in this run:

1. Resolve exemplar entities to `ExemplarRef` tuples via `archigraph_inspect(label_or_id=<entity_name>)` for entity-name + `archigraph_get_source` for file path + line range.
2. Resolve all outgoing pattern-relationship edges via `archigraph_expand(node=<pattern_id>, depth=1)`:
   - **`SUPERSEDES`** → RelatedPattern (this pattern replaces the linked one).
   - **`CO_APPLIES_WITH`** → RelatedPattern (typically applied together).
   - **`PREREQUISITE`** → RelatedPattern (the linked pattern must be satisfied first).
   - **`CONFLICTS_WITH`** → mention in a "Conflicts" callout (these two patterns cannot both apply to the same target).
   - **`EXEMPLAR`** → additional exemplar entities (code examples of the pattern in use).
   - **`ANTI_EXEMPLAR`** → additional anti-pattern exemplars (code examples of what NOT to do; omit if the entity's `private=true`).
   - **`TOUCHES`** → entities the pattern's steps read or modify (listed in the "Recipe" section as context, not as exemplars).
   - **`CREATED_BY`** (incoming, from Entity to Pattern) → this is written by `apply` when a pattern is used, not emitted here; do not follow it in this pass.
   Convert `SUPERSEDES` / `CO_APPLIES_WITH` / `PREREQUISITE` hits into `RelatedPattern` entries for the "Related patterns" section.
3. Call `WriteMarkdown(<docs_root>, MarkdownInput{Pattern: p, ExemplarRefs: ..., RelatedPatterns: ...})`. The renderer:
   - Skips when `is_candidate=true` (returns empty markdown — caller is expected to no-op).
   - Strips private anti-patterns.
   - Auto-backticks code identifiers in headings + body.
   - Writes to `<docs_root>/<category>/<id>.md` atomically.
4. After writing, run `CheckBacktickConvention` on the produced markdown. If violations are reported, fail the pass with the exact heading line — do NOT silently re-write.

## Repair candidate emission

After all pattern docs for this run are written, run the emission step from
`snippets/docgen-repair-emission.md`. Pattern documentation is a specialised
source of repair candidates because exemplar resolution (Step 1 of the
Procedure) reads source files and inspects entity neighborhoods — exactly the
context that surfaces mis-classifications and unresolved stubs.

Primary discovery types in this pass:

- **`fix_kind`** — exemplar entities are often mis-classified. When you call
  `archigraph_inspect` on an exemplar and see it is, say, a Kafka topic
  struct catalogued as `Class`, emit a `fix_kind` candidate.

  Example:

  ```json
  {
    "type": "fix_kind",
    "source_entity_id": "<UserEventTopic entity id>",
    "new_kind": "MessageTopic",
    "confidence": 0.90,
    "evidence": "events/user_event_topic.go@line 3: var UserEventTopic = kafka.Topic{Name: \"user.events\"} — topic definition misclassified as Class",
    "source": "generate-docs/pass-12",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

- **`merge_flow`** — pattern detection sometimes surfaces two flow entities
  that represent the same workflow (e.g. a canary variant and a stable variant
  with the same business outcome). When you see this while building the
  exemplar table, emit a `merge_flow` candidate.

  Example:

  ```json
  {
    "type": "merge_flow",
    "source_entity_id": "<checkout_flow entity id>",
    "target": "<checkout_legacy_flow entity id>",
    "confidence": 0.80,
    "evidence": "checkout_handler.go@line 71: A/B flag routes to checkout_flow or checkout_legacy_flow — same business outcome, different entry points",
    "source": "generate-docs/pass-12",
    "emitted_at": "<ISO 8601 timestamp>"
  }
  ```

Use `source: "generate-docs/pass-12"` in all candidates. Append to
`~/.archigraph/groups/<group>/docgen-repairs.jsonl`.

## Constraints

- DO NOT generate docs for `is_candidate=true` patterns. The renderer skips them; this is a hard invariant.
- DO NOT omit the `Status`, `Category`, `Confidence` front-matter block. Downstream tooling parses it.
- DO NOT inline private anti-patterns under any circumstance. Tests in `internal/agentpatterns/docs_test.go` enforce this.
- DO NOT skip Pass 11 — without `documentation_url`, the graph cannot link prose to pattern docs.
