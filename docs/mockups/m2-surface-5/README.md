# Surface 5 — Docs Portal: Mockup Package

**Sprint:** Milestone 2
**Date:** 2026-05-20
**Status:** Design complete, pending frontend implementation

---

## Files in this directory

| File | Description |
|---|---|
| `docs-landing.png` | View 1: Doc landing page (sidebar nav tree, content area, right-rail TOC) |
| `docs-content.png` | View 2: Doc page with code blocks + hovercard preview + View 3: search overlay + View 4: pattern callout inline |
| `docs-themes.png` | Light vs dark theme side-by-side + mobile narrow viewport treatment |
| `mockup-docs-landing.svg` | Source SVG — editable |
| `mockup-docs-content.svg` | Source SVG — editable |
| `mockup-docs-themes.svg` | Source SVG — editable |

---

## React component → plan section mapping

| Visual element | React component (§4/§5) | File location |
|---|---|---|
| Top-level layout shell | `<DocsPage>` | `components/docs/DocsPage.tsx` |
| Left sidebar tree | `<DocsSidebar>` | `components/docs/DocsSidebar.tsx` |
| Single tree node | `<DocsSidebarItem>` | `components/docs/DocsSidebarItem.tsx` |
| Content column | `<DocsContent>` | `components/docs/DocsContent.tsx` |
| Markdown renderer | `<MarkdownRenderer>` | `components/docs/MarkdownRenderer.tsx` |
| Prism code block | `<CodeBlock>` | `components/docs/CodeBlock.tsx` |
| Lazy-loaded mermaid | `<MermaidBlock>` | `components/docs/MermaidBlock.tsx` |
| Backtick symbol deep-link | `<EntityLink>` / `<CodeSymbolLink>` | `components/docs/EntityLink.tsx` |
| Mini entity card on hover | `<EntityHovercard>` | `components/docs/EntityHovercard.tsx` |
| Pattern recipe callout | `<PatternCallout>` | `components/docs/PatternCallout.tsx` |
| Process flow inline | `<FlowDiagram>` | `components/docs/FlowDiagram.tsx` |
| Breadcrumb trail | `<DocsBreadcrumbs>` | `components/docs/DocsBreadcrumbs.tsx` |
| Right-rail TOC + scroll-spy | `<DocsTOC>` | `components/docs/DocsTOC.tsx` |
| Prev/Next footer nav | `<DocsPrevNext>` | `components/docs/DocsPrevNext.tsx` |
| Top search input + overlay | `<DocsSearch>` | `components/docs/DocsSearch.tsx` |
| Search result item | `<DocsSearchResult>` | `components/docs/DocsSearchResult.tsx` |
| Light/dark toggle | `<ThemeToggle>` | `components/docs/ThemeToggle.tsx` |
| Reading progress bar | Inline in `<DocsContent>` scroll handler | `components/docs/DocsContent.tsx` |

**Total atomic components: 18**

---

## Hook contracts

| Hook | Responsibility |
|---|---|
| `useDocTree(group)` | `GET /api/docs/{group}` — sidebar tree + recent files |
| `useDocContent(group, path)` | `GET /api/docs/{group}/{path}?include=hovercards` — markdown + breadcrumbs + prev/next + hovercard data |
| `useDocsSearch(group, query)` | `GET /api/search/{group}?q=...&type=docs` — search results with snippet + score |
| `useTocScrollSpy(headings)` | Intersection Observer on heading elements; returns active heading ID |
| `useTheme()` | `localStorage.getItem('theme')` → CSS var sync; `prefers-color-scheme` fallback |
| `useEntityHovercard(entityId)` | Lazy-fetch or read from pre-resolved hovercards map (from `include=hovercards` response) |
| `useEntityDeepLink()` | Build `/graph?entity=<id>` URL with camera hint |

---

## Layout grid

```
[nav: 48px height, full width]
[progress bar: 3px, full content width]

[sidebar: 260px] | [content: flex-1, max-width ~800px] | [TOC: 260px]

  content padding: 48px left/right (within content column)
  content line-length cap: ~75ch (~800px at 14px body)
  section spacing: 32px between heading groups
  code block margin: 24px top/bottom
```

Mobile (≤768px): sidebar hidden → Sheet drawer (hamburger), TOC collapsed → inline accordion, single column layout, no right rail.

---

## Typography

All typography follows Surface 4 baseline:
- **Headings:** Inter, font-weight 700, letter-spacing -0.4px
  - H1: 28px / 1.2 (page title)
  - H2: 20px / 1.3 (major sections)
  - H3: 16px / 1.4 (sub-sections)
- **Body:** Inter 14px / 1.6 / `#94A3B8` (dark) / `#475569` (light)
- **Code inline:** JetBrains Mono 13px — in `<EntityLink>` pill, `#22C55E` foreground
- **Code block:** JetBrains Mono 11px, line-height 1.8, with line numbers
- **Captions:** Inter 10px italic, `#475569`

---

## Interaction notes

### EntityLink hover (backticked symbols)
- Hover `UserPermission` inline code token → `<EntityHovercard>` appears below token
- **[ANIM]** Hovercard: 150ms fade-in + scale from 0.95→1.0, ease-out
- Card shows: kind badge, source file, start line, top 3 outbound edges
- Click hovercard → `useEntityDeepLink()` → navigate to `/graph?entity=<id>`
- Keyboard: `Tab` to entity link, `Enter` to navigate to graph

### Search overlay (⌘K)
- **[ANIM]** Overlay: backdrop `opacity 0→0.72` + blur 4px, 200ms; modal `scale 0.96→1.0 + opacity 0→1`, 200ms
- Typeahead: debounce 200ms, server-side `GET /api/search/{group}?q=...`
- Result tabs: All / Docs / Entities / API paths
- Entity results show "Open in Graph →" deep-link
- API results show "Open in API →" deep-link to Surface 4
- **[ANIM]** Result hover: bg highlight 80ms ease
- Keyboard: `↑↓` navigate, `Enter` select, `Esc` close

### TOC scroll-spy
- `IntersectionObserver` watches all h2/h3 elements
- Active heading: 2px left border `#22C55E`, bg `#162840`
- **[ANIM]** Active transition: 80ms ease on active ID change
- Smooth scroll to anchor on TOC click: `scroll-behavior: smooth`

### Reading progress bar
- `scrollY / (documentHeight - viewportHeight)` → width percentage
- Updates on every scroll event (rAF-throttled)
- Color: `#22C55E`; no animation on the bar itself (just width)

### Sidebar tree collapse/expand
- **[ANIM]** Accordion: `height` transition 200ms ease-in-out
- Expansion state persisted in URL: `?sidebar=<base64-encoded-tree-state>`

### Pattern callout (`{{pattern:*}}`)
- Rendered by `<PatternCallout>` component
- Exemplar chips are clickable → navigate to that path in Surface 4 (`/api?prefix=...`)
- "View violations →" opens Surface 4 filtered to violating paths

### Flow diagram inline (`{{flow:*}}`)
- `<FlowDiagram>` lazy-fetches process chain via `useFlowDetail`
- Renders mermaid sequence/flowchart diagram
- Theme-aware: Mermaid `themeVariables` read from CSS vars
- **[ANIM]** mermaid: lazy-loaded on first appearance (dynamic import), 300ms fade-in after render

### Theme toggle
- Click sun/moon → `useTheme().toggle()`
- Sets `data-theme="light"|"dark"` on `<html>`
- Persists to `localStorage`
- **[ANIM]** Theme switch: CSS transition on all `--` vars, 200ms ease

---

## Light mode token values

| Dark token | Dark value | Light value |
|---|---|---|
| `--bg-base` | `#0E1117` | `#FAFAFA` |
| `--bg-surface` | `#0C1118` | `#FFFFFF` |
| `--bg-elevated` | `#141920` | `#F1F5F9` |
| `--bg-code` | `#080D14` | `#F8FAFC` |
| `--border-subtle` | `#1E2733` | `#E2E8F0` |
| `--text-primary` | `#E2E8F0` | `#0F172A` |
| `--text-secondary` | `#94A3B8` | `#475569` |
| `--text-muted` | `#64748B` | `#94A3B8` |
| `--brand-green` | `#22C55E` | `#16A34A` (darkened for WCAG AA on white) |
| `--brand-green-bg` | `#172A1F` | `#F0FDF4` |
| Verb chip backgrounds | dark tints | light tints (opacity 0.08 on white) |

---

## Mobile breakpoint behavior

At ≤768px:
- **Sidebar:** hidden by default; hamburger icon in nav opens Radix UI `<Sheet>` drawer from left (250ms ease-out translateX)
- **Right rail TOC:** removed from layout; replaced with an inline `<details>` element above content ("On this page ▾")
- **Nav:** search icon → opens search overlay; no inline search bar (saves width)
- **Code blocks:** `overflow-x: auto` with scroll indicator (thin blue bar at bottom of block)
- **Content:** full-width with 16px horizontal padding
- **Reading progress:** stays at top (still useful)

---

## Spec ambiguities

1. **`/api/docs/{group}/{path}` markdown file source.** The plan says the server "serves markdown files from the repo's doc path." The exact file discovery logic is not defined: does it scan `docs/` directories? README files? Files matching `*.md`? The sidebar tree shape depends on this. Recommendation: scan `{repo_root}/docs/**/*.md` + root `README.md`. Add to REST catalog.

2. **`{{pattern:*}}` syntax parsing.** The plan mentions this callout syntax but doesn't specify how the markdown renderer detects it. Recommendation: implement as a remark plugin that matches the regex `/\{\{pattern:([^}]+)\}\}/g` and replaces with `<PatternCallout id="...">`. The remark plugin needs access to the pattern store (pre-resolved server-side in the `include=hovercards` response or via a separate `GET /api/patterns/{group}/{id}` call).

3. **`{{flow:*}}` rendering requires process chain data.** `<FlowDiagram>` must call `useFlowDetail(group, entryEntityId)` which in turn calls `GET /api/flows/{group}/{processId}`. The entity ID inside `{{flow:...}}` must be a `Process` entity ID, not a function ID. Documentation authors need to know this constraint.

4. **Cross-repo `<repo>::<heading>` link syntax is mentioned in the plan but not fully specified.** The plan says "server-resolved at render time." This implies the markdown-rendering endpoint pre-resolves these links before returning the content to the client, or the client gets a map of `{repo::heading → resolved_url}` in the page response. Implementation approach not specified.

5. **Entity hovercard refresh frequency.** Hovercards pre-fetched via `?include=hovercards` are stale once fetched. If the user re-indexes the codebase while viewing a doc page, hovercards will be outdated. The `/ws/events` watcher should trigger a React Query invalidation of `useDocContent` so hovercards refresh. This WebSocket integration with docs is not mentioned in the plan.

6. **Prism language support.** The plan lists JS, Python, Go, SQL, Bash, YAML as the bundled language set. The fixture repos are Python/JS. If fixture-c (mobile, presumably Swift/Kotlin) has code blocks, those languages are not in the initial bundle. Recommend adding Swift and Kotlin to the initial language set.

---

*Designed by BB-8 (Design Lead) · 2026-05-20*
