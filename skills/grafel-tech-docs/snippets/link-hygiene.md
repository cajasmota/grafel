# Link hygiene

Fixes the "37 broken links" class from the 2026-05-23 docgen audit. Every
markdown link a writer emits must satisfy these rules. Pass 8
(`prompts/08-cross-link.md`) validates them across the whole tree; the
verification checklist gates them per file as they are written.

## 1. A link target must be a real, generated doc file

Before emitting `[text](path)`, the producer must be able to point at the doc
file that `path` resolves to. If you cannot, do not emit a link — write the
identifier in backticks instead (the ADR-0007 bridge), which is clickable-free
but graph-valid.

## 2. Never link into source-code directories

`../../src/network/hooks/`, `../../core/...`, `../../dockerfile` are **not doc
pages**. The audit found these emitted as if they were links. A source file is
referenced in **backticks as a path**, never as a markdown link:

```markdown
Defined in `core/views/report_viewset.py`.        ✅
See [report viewset](../../core/views/report_viewset.py).   ❌
```

The only links allowed are to files under a `docs/` tree.

## 3. Directory links must target an index file that exists

A bare-directory link (`[modules](modules/)`, `[reference](reference/)`) 404s
unless that directory has an `index.md` / `README.md`. Two options, in order:

1. Link to a concrete page inside the directory
   (`[API reference](reference/api.md)`), OR
2. If you genuinely mean "the section", link to a generated index file
   (`[modules](modules/README.md)`) **and ensure that index file is in the
   plan**. Pass 2 only schedules an index page for a section that has ≥ 1 child
   page; do not link to an index that was never generated.

Never emit `](modules/)` with a trailing slash and no file.

## 4. Use the filesystem dirname for paths, the slug for prose

The registry slug (`upvate-core`, hyphenated) and the on-disk directory
(`upvate_core`, underscored) can differ. This caused
`core-mobile/docs/overview.md` → `../../upvate-core/docs/` (404, dir is
`upvate_core`).

- **Relative link paths** (`../../<dir>/docs/...`) MUST use the actual
  filesystem directory name. Resolve it from `grafel_orient (view=me)` /
  `inventory.json` `repo.path` (the last path segment), not from the slug.
- **Prose / display text** may use the human slug.

```markdown
See the [billing overview](../../upvate_core/docs/overview.md) in `upvate-core`.
                              └ filesystem dirname ┘            └ slug in prose ┘
```

## 5. Anchors must satisfy the anchor contract

A link `path#anchor` must point at a heading whose slug (per
`snippets/anchor-contract.md`) equals `anchor`. Same slugification rule on both
sides.

## 6. Don't link to optional pages that were not generated

`how-to/local-dev.md`, a `reference/` index, etc. are optional. Only link to one
after confirming the plan scheduled it. If it was skipped, drop the link.
