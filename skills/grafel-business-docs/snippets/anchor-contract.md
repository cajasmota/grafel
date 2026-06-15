# Anchor contract (shared slug vocabulary)

Fixes the class of bug where a page declares `anchors:` in its frontmatter but
the prose uses different headings, so the declared anchor slugs do not resolve
(the "17 anchor mismatches" found in the 2026-05-23 docgen audit). The root
cause was that the anchor generator and the heading writer used **different
vocabularies** (`summary` declared, `## Where it lives` written).

There is exactly one rule and it has no exceptions.

## Rule: derive `anchors:` from headings, never the other way around

A writer subagent must **write the prose headings first, then derive the
`anchors:` list from those headings.** Never hand-author an `anchors:` list and
hope the headings match it.

### Slugification (must match the backend + Pass 8 checker exactly)

```
slug(heading) =
  1. take the heading text AFTER the leading `#`+space
  2. strip surrounding backticks but KEEP the inner text
       "## How `JWTAuthMiddleware` validates" → "How JWTAuthMiddleware validates"
  3. lowercase
  4. replace every run of non-alphanumeric characters with a single "-"
  5. trim leading/trailing "-"
```

Worked examples:

| Heading | Slug |
|---------|------|
| `## Summary` | `summary` |
| `## Where it lives` | `where-it-lives` |
| `## How it's used` | `how-it-s-used` |
| ``## How `JWTAuthMiddleware` validates tokens`` | `how-jwtauthmiddleware-validates-tokens` |
| `## Business rules` | `business-rules` |

### Procedure for any writer that emits `anchors:` frontmatter

```
1. Write the full markdown body (all `##`/`###` headings + prose).
2. Collect every heading line.
3. anchors = [ slug(h) for h in headings ]   # in document order, deduped
4. Write the frontmatter `anchors:` list to exactly that derived set.
5. Self-check: for each declared anchor, grep the body for a heading whose
   slug() equals it. If any anchor has no matching heading → STOP, you built
   the list by hand. Re-derive from step 2.
```

### Do NOT

- Do **not** copy an `anchors:` template list (`[summary, primary-implementation,
  patterns, consumers, gotchas]`) and then write whatever headings feel natural.
  If you use a template heading set, you must write **those exact headings**.
- Do **not** include an anchor for a heading you did not write.
- Do **not** emit `anchors:` at all if the page has no headings worth anchoring —
  an empty/absent list is valid; a wrong list is a hard failure.

Pass 8 (`prompts/08-cross-link.md`) re-runs this check across all pages and the
verification checklist (`snippets/verification-checklist.md`) gates it per file.
