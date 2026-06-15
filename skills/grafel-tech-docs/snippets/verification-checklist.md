# Verification checklist

Run this checklist on every markdown file produced by any pass. The orchestrator re-runs it before accepting the pass output. A failed check is a hard stop — fix and re-run.

## Backtick discipline (ADR-0007)

- [ ] Every heading containing a CamelCase or `snake_case` word has that word in backticks.
- [ ] Every prose mention of a code identifier has it in backticks.
- [ ] Every file-path mention is in backticks (not a markdown link, unless the link target is a real doc page).

A useful regex to flag candidate violations (catches CamelCase or snake_case outside backticks in headings):

```bash
grep -nE '^#+ .*[^`]\b([A-Z][a-zA-Z0-9]*[a-z][a-zA-Z0-9]*[A-Z][a-zA-Z0-9]*|[a-z]+_[a-z_]+)\b' <file>
```

False positives are possible (acronyms, capitalized prose words) — review each match before "fixing". The check is "review, then decide", not blind regex replace.

## Code-block tags

- [ ] Every fenced code block has a language tag.
- [ ] Languages match the actual content (no `bash` blocks containing Python, etc.).

## Structural

- [ ] No empty sections — either fill them or delete them.
- [ ] No empty stub pages — never emit a `flows/index.md` (or any page) that contains only a heading or a placeholder table with no rows. If a module has no flows, do not write a `flows` page at all (see `prompts/02-plan.md` § Volume control).
- [ ] No placeholder text remaining (`<one line>`, `<entity>`, `TODO`, `FIXME`).
- [ ] Tables have a header row + alignment row.
- [ ] Internal links use relative paths and resolve to existing files.

## Links + anchors (`snippets/link-hygiene.md`, `snippets/anchor-contract.md`)

- [ ] No link points into a source-code directory (`src/`, `core/`, `dockerfile`, …). Source paths are backticked, not linked.
- [ ] No bare-directory link (`](modules/)`) without a real index-file target that exists.
- [ ] Relative link PATHS use the filesystem dirname; prose may use the slug. (Catches the `upvate-core` vs `upvate_core` 404.)
- [ ] No link to an optional page the plan did not schedule.
- [ ] If the file declares `anchors:` in frontmatter, every declared anchor has a matching heading IN THIS FILE whose slug equals it. The list was derived FROM the headings, not hand-authored.

## Forbidden terms

- [ ] No mention of any prior tooling name. Only `grafel` is the tool's name in the doc text.

## Source-snippet hygiene (Pass 4 mostly)

- [ ] Every quoted source snippet cites its file path in backticks.
- [ ] No snippet exceeds ~10 lines unless absolutely necessary.
- [ ] No snippet contains secrets, real customer data, or proprietary credentials.

## Cross-repo links (Pass 8 will validate fully)

- [ ] Every cross-repo link follows `cross-link-format.md`.
- [ ] Pending link candidates are marked clearly as unconfirmed.

## Plan adherence

- [ ] The file's section headings match the relevant `output-templates/*.md`.
- [ ] No drive-by additions of sections the template did not include.

## Business tier only (`snippets/business-voice.md`)

Apply these checks to any file under a `business/` directory. A violation is a hard stop — rewrite, do not ship.

- [ ] Zero internal symbol names in the visible body (no class/function/file names, no API paths, no env vars, no table names, no module slugs, no repo dir names). The ONLY place a symbol/path may appear is inside the collapsed `<details>` provenance block.
- [ ] No `sequenceDiagram` / `classDiagram` / call-graph mermaid. Any mermaid is a business-step `flowchart` with ≤ 8 business-labelled boxes.
- [ ] No quoted source code blocks.
- [ ] A non-engineer could read the page top to bottom and understand it without help.
- [ ] Every link resolves to another `business/` page or the domain glossary (run after the page's siblings exist).
