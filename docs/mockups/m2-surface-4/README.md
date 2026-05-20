# Surface 4: API & Contracts Explorer — Mockup Package

**Sprint:** Milestone 2
**Date:** 2026-05-20
**Status:** Design complete, pending frontend implementation

---

## Files in this directory

| File | Description |
|---|---|
| `path-list.png` | Path list page mockup (dark mode) |
| `path-detail.png` | Path detail page with Handlers tab open |
| `multi-impl-banner.png` | Multi-implementation divergence banner + expanded comparison view + empty/loading/error states |
| `mockup-path-list.svg` | Source SVG for path-list (editable) |
| `mockup-path-detail.svg` | Source SVG for path-detail (editable) |
| `mockup-multi-impl-banner.svg` | Source SVG for banner + states (editable) |
| `component-spec.md` | Full atomic component specification — read this before implementing |

---

## What to implement — ordered priority

1. **`<VerbChip>`** — used everywhere, implement first. See `component-spec.md §1`.
2. **`<MultiplicityBadge>`** — trivial pill, implement alongside VerbChip.
3. **`<PathRow>`** + `<PathRowSkeleton>` — the list item unit.
4. **`<PathSearchInput>`** + **`<PathFilterPanel>`** — filter controls.
5. **`<PathTreeSidebar>`** — left rail tree.
6. **`<Pagination>`** — shared component, lives in `components/shared/`.
7. **`<QuickStatsCard>`** — right rail, derived data only.
8. **Path list route** (`/api`) — assembles all of the above.
9. **`<HandlerCard>`** — source snippet viewer.
10. **`<ResponseShapeGrid>`** + `<DynamicResponseNotice>`.
11. **`<InboundFetchList>`** + **`<OutboundQueryList>`**.
12. **`<PathDetailPage>`** route — tab container.
13. **`<MultiImplBanner>`** + **`<MultiImplComparisonPanel>`** — conditional banner.

---

## React component → plan section mapping

| Component (this doc) | Plan §4 name | File location |
|---|---|---|
| `<VerbChip>` | `<VerbChip>` | `components/api-explorer/VerbChip.tsx` |
| `<MultiplicityBadge>` | `<MultiplicityBadge>` | `components/api-explorer/MultiplicityBadge.tsx` |
| `<WebhookBadge>` | (new, not in plan) | `components/api-explorer/WebhookBadge.tsx` |
| `<PathRow>` | `<PathRow>` | `components/api-explorer/PathRow.tsx` |
| `<PathTreeSidebar>` | `<PathTreeSidebar>` | `components/api-explorer/PathTreeSidebar.tsx` |
| `<PathSearchInput>` | `<PathSearchInput>` | `components/api-explorer/PathSearchInput.tsx` |
| `<PathFilterPanel>` | `<PathFilterPanel>` | `components/api-explorer/PathFilterPanel.tsx` |
| `<Pagination>` | `<Pagination>` | `components/shared/Pagination.tsx` |
| `<QuickStatsCard>` | (new, not in plan — derive from PathStats) | `components/api-explorer/QuickStatsCard.tsx` |
| `<PathDetailPage>` | `<PathDetailPage>` | `routes/PathDetail.tsx` |
| `<HandlerCard>` | (not named in plan, backs PathDetailPage) | `components/api-explorer/HandlerCard.tsx` |
| `<ResponseShapeGrid>` | `<ResponseShapeGrid>` | `components/api-explorer/ResponseShapeGrid.tsx` |
| `<DynamicResponseNotice>` | (implicit in ResponseShapeGrid) | `components/api-explorer/DynamicResponseNotice.tsx` |
| `<InboundFetchList>` | `<InboundFetchList>` | `components/api-explorer/InboundFetchList.tsx` |
| `<OutboundQueryList>` | `<OutboundQueryList>` | `components/api-explorer/OutboundQueryList.tsx` |
| `<MultiImplBanner>` | `<MultiImplBanner>` | `components/api-explorer/MultiImplBanner.tsx` |
| `<MultiImplComparisonPanel>` | (expansion of MultiImplBanner) | `components/api-explorer/MultiImplComparisonPanel.tsx` |

**Total atomic components: 17**

---

## Hooks → endpoint mapping

| Hook | Endpoint | Notes |
|---|---|---|
| `usePathList(group, filters, page)` | `GET /api/paths/{group}?page=N&size=50&...` | Server-side pagination + filtering |
| `usePathTree(group)` | (derived from usePathList cache) | No extra fetch |
| `usePathDetail(group, pathHash)` | `GET /api/paths/{group}/{pathHash}` | Combined shape: handlers + shapes + fetches + queries |
| `usePathFilters()` | (URL params only) | Drives usePathList refetch |
| `useResponseShape(entityId)` | (pure derivation) | No fetch |

---

## Aesthetic decisions

**Typography:** Inter (UI labels, metadata) + JetBrains Mono (paths, code, handler names, table names). These are non-negotiable — path strings MUST render in monospace to be scannable.

**Brand accent:** `#22C55E` (green-500). Used for: active sidebar item, active page in pagination, search caret, well-covered endpoint badge. NOT used for decorative color or backgrounds.

**Dark-mode default:** all mockups show dark mode. Light mode inverts CSS variables only — no component logic changes. See `component-spec.md` light mode section.

**Density:** power-user audience. 52px path rows is the minimum comfortable height with 2-line content. Do not increase row height. Sidebar items are 22px (single-line). Resist adding padding "for breathing room" — this is a data-dense tool.

**Verb chips:** the distinct color per verb is the single most important design decision on this surface. A developer must be able to scan a list of paths and read the verb distribution in under 1 second. The colors are specified in component-spec.md and are NOT subject to change.

---

## Spec ambiguities (noted for resolution)

1. **`pathHash` format not specified in plan §4.** The endpoint is `GET /api/paths/{group}/{pathHash}`. The hash function for the path string is not defined. Recommendation: SHA-256 first 8 bytes of `{verb}:{path}` URL-encoded. Backend team to confirm before frontend routing is wired.

2. **`MultiImplBanner` trigger condition is underspecified.** The plan says "when fetches_count > 1" but that alone doesn't mean divergence (two callers using the same verb+payload is fine). The design models the trigger as: `fetches_count > 1 AND count of distinct verbs used by callers > 1`. Backend should compute `has_impl_divergence: boolean` in the PathDetail response to avoid frontend having to re-derive from the FETCHES list.

3. **Response shape extraction coverage is unknown for fixture-a.** The audit says 529 unique verb-path pairs but doesn't report what percentage have `response_keys`. The `<QuickStatsCard>` shows a placeholder "412 / 648" — this should be computed from the actual aggregator in `GET /api/paths/{group}` stats field.

4. **Source file deep-link target.** The `<HandlerCard>` source file line (e.g. `core/viewsets/user_viewset.py:L89`) should link somewhere — either a VS Code deep-link (`vscode://file/...`) or a GitHub URL if repo has a remote. This behavior is not specified in the plan. Design shows it as clickable text; implementation behavior TBD.

5. **Admin gap display.** The fixture-a audit found ~310 Django admin routes are absent from the graph. The sidebar `/admin/` node shows a "~310 gap" amber badge. This count should come from the backend stats, not be hardcoded. Add `admin_unindexed_count: number` to the `GET /api/paths/{group}` response stats field.

---

## Pencil source note

The Pencil MCP was not available in this session (MCP server binary exists at `/Applications/Pencil.app/Contents/Resources/app.asar.unpacked/out/mcp-server-darwin-arm64` but was not connected to this Claude session). The mockups were designed as high-fidelity SVGs using the same component decomposition and visual language that would be used in Pencil. SVG sources are fully editable and can be imported into Pencil via File > Import or converted to Pencil components when the MCP is available in the next session.

---

*Designed by BB-8 (Design Lead) · 2026-05-20*
