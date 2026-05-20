# Surface 4 — API & Contracts Explorer: Component Specification

**Date:** 2026-05-20
**Designer:** BB-8 (Design Lead)
**Spec source:** milestone-2-dashboard-plan.md §4 + fixture-a endpoint audit
**Frontend agent:** this document is your implementation contract

---

## Scale facts (from fixture-a audit — use these in your skeleton/pagination logic)

| Metric | Value |
|---|---|
| Total http_endpoint entities | 1,212 (post-dedup: ~529 unique verb-path pairs) |
| Unique path strings | 337 (graph.json) / 648 (estimated pre-dedup) |
| Default page size | 50 rows |
| Page count at 648 paths | 13 pages |
| Paths with 5 endpoints (full CRUD ViewSet) | ~90 |
| Single-entity paths | ~444 |
| Paths with ANY verb (unresolved) | 46 (suppressed by aggregator per §4 noise rule) |
| React frontend callers (fixture-b) | ~301 unique endpoint templates |
| Mobile frontend callers (fixture-c) | ~30 unique endpoint templates |

---

## Atomic Component Inventory

### 1. `<VerbChip>` — `components/api-explorer/VerbChip.tsx`

**Visual:** Pill/capsule shape, 36-54px wide × 18px tall, 9px border-radius.
**Colors:**
- GET: bg `#0D2418`, border `#10B981`, text `#10B981` (Tailwind: green-500)
- POST: bg `#0D1A2E`, border `#3B82F6`, text `#3B82F6` (blue-500)
- PUT: bg `#1B1000`, border `#F59E0B`, text `#F59E0B` (amber-500)
- PATCH: bg `#1A1030`, border `#8B5CF6`, text `#8B5CF6` (violet-500)
- DELETE: bg `#1E0808`, border `#EF4444`, text `#EF4444` (red-500)
- ANY: bg `#1A2230`, border `#64748B`, text `#64748B` (slate-500)
- WS: bg `#1A0D30`, border `#A855F7`, text `#A855F7` (purple-500)

**Typography:** JetBrains Mono, 10px, font-weight 700, uppercase.
**Props:** `verb: "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "ANY" | "WS"`, `size?: "sm" | "md"` (sm=16px tall for use inside list rows, md=22px tall for path detail header).
**Behavior:** `onClick?: (verb) => void` — navigates to PathDetailPage filtered to that verb+path combo.
**Animation:** hover: scale 1.02, 100ms ease. No other animation.
**Accessibility:** `role="button"`, `aria-label="Filter by {verb}"`, focus ring using brand green.

---

### 2. `<MultiplicityBadge>` — `components/api-explorer/MultiplicityBadge.tsx`

**Visual:** Subtle gray pill, 70px wide × 18px tall, 9px border-radius. Hidden when count === 1.
**Colors:** bg `#1A2230`, border `#2D3748`, text `#94A3B8` (slate-400).
**Typography:** Inter, 10px, normal weight.
**Content:** `"{count} endpoints"` — e.g. "5 endpoints", "3 endpoints".
**Props:** `count: number` — component renders null when count === 1.
**Purpose:** Communicates DRF ViewSet CRUD expansion (one path → 5 entities). Not a "warning", just informational.

---

### 3. `<WebhookBadge>` — `components/api-explorer/WebhookBadge.tsx`

**Visual:** Small amber pill, 50px wide × 18px tall. Shows provider name.
**Colors:** bg `#1B1208`, border `#D97706`, text `#D97706`.
**Typography:** Inter, 10px.
**Props:** `provider: string` — e.g. "Mailgun", "GitHub", "Stripe". Empty string = generic "Webhook" label.
**Placement:** Right side of PathRow, right of framework text.

---

### 4. `<PathRow>` — `components/api-explorer/PathRow.tsx`

**Visual:** 52px tall row. Left: path string (JetBrains Mono 13px, font-weight 500). Below path: handler class name (Inter 11px, `#475569`). Middle-right: VerbChip set. Right of verbs: MultiplicityBadge. Rightmost: `→` affordance (14px, `#334155`).
**URL params in path:** `{id}`, `{pk}`, etc. rendered in `#64748B` (muted, same monospace font).
**Hover state:** bg transitions from `#0E1117` to `#141920`, 80ms ease-in. Arrow `→` shifts 4px right, 80ms ease.
**Click:** navigate to `/paths/{group}/{pathHash}`.
**Props:** `path: PathRowData` (see hook types), `onClick: (pathHash: string) => void`.
**Loading state:** Render `<PathRowSkeleton>` — two gray shimmer bars (280px wide + 140px wide), 14px tall each, 6px gap, 80ms stagger.

---

### 5. `<PathTreeSidebar>` — `components/api-explorer/PathTreeSidebar.tsx`

**Width:** 220px (fixed).
**Structure:** Collapsible tree. Parent nodes show count badge (right-aligned gray pill). Active leaf shows 2px left border in brand green (`#22C55E`), bg `#162840`.
**Collapse chevron:** `▾` (open) / `▶` (collapsed), `#94A3B8`, 12px.
**Active state:** text color `#22C55E`, count badge colors flip to green-tinted bg.
**Scrollable:** overflow-y auto within the sidebar height.
**Props:** `tree: PathTreeNode[]`, `activePrefixGroup: string`, `onSelect: (prefix: string) => void`.
**Data source:** `tree` field from `usePathList` cache — no extra fetch.
**Special case:** Show admin gap notice: `/admin/` node with amber tint + "~310 gap" badge + tooltip "archigraph does not yet extract Django admin URL patterns (issue #792)".
**Animation:** Accordion open/close: 200ms height transition, ease-in-out.

---

### 6. `<PathSearchInput>` — `components/api-explorer/PathSearchInput.tsx`

**Visual:** 920px wide, 38px tall, rounded-lg (8px). Dark bg `#141920`, border `#1E2733`, text `#E2E8F0`, placeholder text `#475569`.
**Search icon:** `⌕` or Lucide `Search`, 16px, `#475569`, 8px left gap.
**Caret:** brand green `#22C55E`.
**Behavior:** Prefix-match typeahead. Debounce: 200ms. Sends to `GET /api/paths/{group}?q={value}` (server-side). Typeahead dropdown: max 8 suggestions, same monospace path styling, 200ms fade-in.
**Clear button:** appears on value !== "", `×` icon, clears field + refetches.
**Props:** Controlled: `value`, `onChange`, `isLoading`.

---

### 7. `<PathFilterPanel>` — `components/api-explorer/PathFilterPanel.tsx`

**Visual:** Horizontal strip of filter chips below the search bar. Each chip: 90-120px wide, 24px tall, 12px border-radius (pill). Active chips: tinted bg + colored text. Inactive: `#1E2733` bg, `#64748B` text.
**Chip types:**
- Repo multi-select: one chip per repo in group (color-coded per repo, same palette as graph surface)
- Framework select: "DRF", "Django", etc.
- Status code filter: e.g. `[404]` chip
- Is-webhook toggle: binary pill
**Remove affordance:** `×` on active chip (8px), 80ms fade.
**Props:** `filters: PathFilters`, `availableRepos: string[]`, `availableFrameworks: string[]`, `onFilterChange: (filters) => void`.
**Hook:** `usePathFilters()` reads/writes URL search params. Changes trigger `usePathList` refetch.

---

### 8. `<Pagination>` — `components/shared/Pagination.tsx`

**Visual:** 52px tall bar. Left: "← Prev" text-link. Center: page number pills (current page highlighted with green bg + border). Right: "Next →" text-link. Below center: "Showing N of M paths" caption.
**Active page:** 28px wide × 24px tall, `#172A1F` bg, `#22C55E` border + text.
**Inactive pages:** text-only, `#475569`.
**Ellipsis:** `...` for gaps between pages 3 and last.
**Props:** `currentPage: number`, `totalPages: number`, `totalItems: number`, `onPageChange: (page: number) => void`.
**Behavior:** Server-side. Page change triggers new fetch, scrolls to top of list.

---

### 9. `<QuickStatsCard>` — `components/api-explorer/QuickStatsCard.tsx`

**Visual:** 276px wide right rail card. Sections: counts (path, verb-pair, @action, webhook), framework breakdown bars, response shape coverage.
**Progress bars:** 200px wide, 6px tall, 3px border-radius. Filled portion uses framework-specific color.
**Typography:** label 11px `#64748B`, value 13px font-weight-600 `#E2E8F0` (or semantic color for warnings).
**Warning values:** red for unresolved ANY verb count + "(issue #792)" caption in `#475569`.
**Props:** `stats: PathStats` (derived from `usePathList` response total fields).

---

### 10. `<PathDetailPage>` — `routes/PathDetail.tsx` + `components/api-explorer/PathDetailPage.tsx`

**Route:** `/paths/{group}/{pathHash}`.
**Layout:** Full page. No secondary sidebar (PathTreeSidebar stays visible). Main content fills remaining width.
**Header card:** 80px tall, `#141920` bg. Left: VerbChips (all verbs for this path). Center: path string large (18px mono). Right: MultiplicityBadge.
**Second line:** repo · framework · status codes (comma-list) · handler class · source file:line.
**Tab bar:** 4 tabs: Handlers | Response Shapes | Inbound (FETCHES) | Outbound (QUERIES). Badge count on Inbound and Outbound tabs.
**Tab animation:** content cross-fade 150ms ease.

---

### 11. `<HandlerCard>` — `components/api-explorer/HandlerCard.tsx`

**Visual:** 590px wide card (two-column grid in Handlers tab). Dark `#0C1118` bg, `#1E2733` border.
**Contents:** VerbChip(s) + handler method name (13px mono) + source file:line (12px `#64748B`). Below: `<SourceSnippet>` preview (3 lines, dark code bg). Below that: status code pills row.
**Source file link:** click → opens source in new tab / deep-links (TBD by backend routing).
**SourceSnippet:** `#080D14` bg, 11px JetBrains Mono, `#64748B` syntax color. 58px tall (3 visible lines). Expand on click: slide-down to full method body, 200ms ease.
**Props:** `handler: HandlerEntity`, `verbs: Verb[]`, `statusCodes: number[]`.

---

### 12. `<ResponseShapeGrid>` — `components/api-explorer/ResponseShapeGrid.tsx`

**Visual:** Grid of key-type pairs. Each key: monospace 11px. Type: small gray chip. Shown in a 2-column grid layout.
**Dynamic response notice:** `<DynamicResponseNotice>` — amber callout "Response shape extracted at runtime only. Keys may vary per request."
**Missing shape notice:** "No response shape extracted" in gray, with reasoning (e.g. "dynamic serializer").
**Props:** `responseKeys: string[]`, `dynamicResponse: boolean`, `entityId: string`.

---

### 13. `<InboundFetchList>` — `components/api-explorer/InboundFetchList.tsx`

**Visual:** Table-like list. Column headers: source file, verb, callsite preview, repo label.
**Row:** 26px tall. Source file in mono 11px. VerbChip (sm size). Callsite code snippet (mono, truncated). Repo badge right-aligned.
**Paginated:** Show 5, "Show all →" link if > 5.
**Empty state:** "No frontend callers found for this path. It may serve internal services only." with a gray illustrative icon.
**Props:** `fetches: InboundFetch[]`.

---

### 14. `<OutboundQueryList>` — `components/api-explorer/OutboundQueryList.tsx`

**Visual:** Row of table chips. Each chip: `#141920` bg, `#1E2733` border, 11px mono text. Chip shows table name (e.g. `core_user`).
**Summary line:** "→ N tables accessed · READS_FROM: X, WRITES_TO: Y"
**Empty state:** "No table queries detected for this handler."
**Props:** `queries: OutboundQuery[]`.

---

### 15. `<MultiImplBanner>` — `components/api-explorer/MultiImplBanner.tsx`

**Visibility trigger:** `fetches_count > 1` AND multiple distinct HTTP verbs across callers.
**Visual:** Full-width banner (1200px), `#181208` bg, `#D97706` border (1.5px). Left: 36px amber icon container. Middle: title + description lines. Right: "View diff →" button.
**Title:** "Multi-client implementation divergence detected" — 13px bold white.
**Body:** Two lines, 12px `#94A3B8`. Calls out specific clients and the diverging verbs.
**Animation:** slide-down from top of content area, 250ms ease-out, on first load when condition is met. Pulse: amber glow on border, 2s loop, on initial appearance only. Reduced-motion: immediate show, no pulse.
**Expanded state:** Below banner, a two-column comparison card (`<MultiImplComparisonPanel>`). Left: client A (method, body fields, status codes, frequency, semantic note). Right: client B (same). "vs" separator between columns.
**Props:** `implementations: ClientImplementation[]`, `defaultExpanded?: boolean`.

---

### 16. `<MultiImplComparisonPanel>` — `components/api-explorer/MultiImplComparisonPanel.tsx`

**Visual:** 1200px wide card, two equal columns. Each column: colored header bar (green for Web, indigo for Mobile), then detail rows.
**Header bar colors:** Web client: `#0F1C14` (green-tinted). Mobile client: `#0D1530` (indigo-tinted).
**Detail rows:** method, body field count, status codes, call frequency, semantic note (green ✓ or amber ⚠).
**Accordion:** hidden by default, revealed by "View diff →" click. Height transition 300ms ease-out.

---

### 17. `<DynamicResponseNotice>` — `components/api-explorer/DynamicResponseNotice.tsx`

**Visual:** Amber callout bar inside ResponseShapeGrid. `#291D08` bg, `#D97706` border left 2px.
**Text:** "Response shape extracted at runtime only. Keys may vary per request." — 11px, `#D97706`.
**Icon:** amber `⚠` left of text.

---

### 18. State components (not standalone components — inline render logic)

#### Empty states

| Context | Copy | Action |
|---|---|---|
| Search returns 0 | "No paths match your filters." | `[Clear filters]` button |
| Group has 0 paths | "This group has no indexed API paths yet." | `[Re-index group]` button |
| Inbound fetches = 0 | "No frontend callers found. This path may serve internal services only." | None |
| Outbound queries = 0 | "No table queries detected for this handler." | None |

#### Loading states

| Context | Behavior |
|---|---|
| Initial path list load | 8 `<PathRowSkeleton>` rows. Each: 280px + 140px gray shimmer bars, staggered 80ms. |
| Path detail load | Header skeleton: 80px gray bar. Tab skeleton: 4 gray pills. Content: 3 `<HandlerCard>` skeletons. |
| Inbound/Outbound tabs | 3 row skeletons, 26px each. |

**Skeleton shimmer:** CSS animation, opacity 0.4 → 0.7 → 0.4, 1.5s ease-in-out loop. Must have `prefers-reduced-motion` fallback (static gray, no animation).

#### Error states

| Context | Display |
|---|---|
| `/api/paths/{group}` fails | Amber-bordered error card: "Failed to load paths. Check archigraph server status." + `[Retry]` button. Auto-retry: 3× with 2s exponential backoff before showing error. |
| Path detail 404 | Full-page: "Path not found." + back link. |
| Path detail 500 | Error card: "Failed to load path detail." + `[Retry]` button. |

---

## Hook contracts (from §4 spec)

```typescript
// usePathList — primary data hook for path list page
usePathList(
  group: string,
  filters: PathFilters,
  page: number
): {
  data: { paths: PathRow[], tree: PathTreeNode[], total: number, hasMore: boolean } | undefined,
  isLoading: boolean,
  isError: boolean,
  error: Error | null
}
// → GET /api/paths/{group}?page=N&size=50&repo=...&framework=...&status_code=...&is_webhook=...&prefix=...&q=...

// usePathTree — derived from usePathList cache, no extra fetch
usePathTree(group: string): PathTreeNode[] | undefined

// usePathDetail — detail page data hook
usePathDetail(
  group: string,
  pathHash: string
): {
  data: PathDetail | undefined,
  isLoading: boolean,
  isError: boolean
}
// → GET /api/paths/{group}/{pathHash}

// usePathFilters — URL param management
usePathFilters(): {
  filters: PathFilters,
  setRepo: (repos: string[]) => void,
  setFramework: (fw: string) => void,
  setStatusCode: (code: number | null) => void,
  setIsWebhook: (v: boolean) => void,
  setPrefix: (prefix: string) => void,
  setQuery: (q: string) => void,
  setPage: (page: number) => void,
  clearAll: () => void
}

// useResponseShape — pure derivation from entity properties (no fetch)
useResponseShape(entityId: string): {
  responseKeys: string[],
  dynamicResponse: boolean
}
```

---

## Color & typography reference

```css
/* Verb chip colors */
--chip-get-bg: #0D2418;    --chip-get-border: #10B981;    --chip-get-text: #10B981;
--chip-post-bg: #0D1A2E;   --chip-post-border: #3B82F6;   --chip-post-text: #3B82F6;
--chip-put-bg: #1B1000;    --chip-put-border: #F59E0B;    --chip-put-text: #F59E0B;
--chip-patch-bg: #1A1030;  --chip-patch-border: #8B5CF6;  --chip-patch-text: #8B5CF6;
--chip-delete-bg: #1E0808; --chip-delete-border: #EF4444; --chip-delete-text: #EF4444;
--chip-any-bg: #1A2230;    --chip-any-border: #64748B;    --chip-any-text: #64748B;
--chip-ws-bg: #1A0D30;     --chip-ws-border: #A855F7;     --chip-ws-text: #A855F7;

/* Surface backgrounds */
--bg-base: #0E1117;
--bg-surface: #0C1118;
--bg-elevated: #141920;
--bg-code: #080D14;

/* Borders */
--border-subtle: #1E2733;
--border-default: #2D3748;

/* Text */
--text-primary: #E2E8F0;
--text-secondary: #94A3B8;
--text-muted: #64748B;
--text-faintest: #475569;

/* Brand */
--brand-green: #22C55E;
--brand-green-bg: #172A1F;

/* Semantic */
--warn-bg: #181208;
--warn-border: #D97706;
--warn-text: #D97706;
--error-bg: #180A0A;
--error-border: #EF4444;
--error-text: #EF4444;
--success-bg: #172A1F;
--success-border: #22C55E;
--success-text: #22C55E;

/* Typography */
--font-ui: 'Inter', system-ui, sans-serif;
--font-mono: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
```

---

## Light mode notes

Light mode is achieved by flipping CSS variables only. The Tailwind `dark:` variant controls which set is active. All component code is theme-agnostic — it uses CSS variables exclusively. When `data-theme="light"` is set on `<html>`:

- `--bg-base` → `#FAFAFA`
- `--bg-surface` → `#FFFFFF`
- `--bg-elevated` → `#F1F5F9`
- `--bg-code` → `#F8FAFC`
- `--border-subtle` → `#E2E8F0`
- `--text-primary` → `#0F172A`
- `--text-secondary` → `#475569`
- `--text-muted` → `#64748B`
- Verb chip backgrounds become lighter tints of the same hue (opacity ~0.1 on white bg)
- Brand green stays identical

---

## Interaction flows

### Selecting a path group from sidebar
1. User clicks `/api/v1/buildings/` in `<PathTreeSidebar>`
2. `onSelect(prefix)` fires
3. `usePathFilters().setPrefix('/api/v1/buildings/')` updates URL: `?prefix=/api/v1/buildings/`
4. `usePathList` refetches with new prefix
5. Main list replaces with skeletons → loaded rows
6. Active indicator animates: previous item loses green border (80ms), new item gains it (80ms)

### Drilling into a path detail
1. User clicks a `<PathRow>` (or clicks `→` affordance)
2. Navigate to `/paths/{group}/{pathHash}`
3. `<PathDetailPage>` mounts, `usePathDetail` fetches
4. Header skeleton shown for 200-500ms while loading
5. On load: header card, tab bar, and default tab (Handlers) render with stagger (each HandlerCard fades in at 50ms offset)

### Opening MultiImplBanner comparison
1. `usePathDetail` data arrives, `implementations.length > 1` is true
2. `<MultiImplBanner>` slides down from top of content (250ms ease-out)
3. User clicks "View diff →"
4. `<MultiImplComparisonPanel>` accordion opens (300ms ease-out)
5. Two columns reveal side-by-side

---

## Admin gap notice (PathTreeSidebar)

When the tree includes `/admin/` node:
- Show count badge with amber styling and "~310 gap" text
- Show tooltip on hover: "archigraph does not yet extract Django admin URL patterns. ~310 admin routes are not indexed. See issue #792."
- No click behavior — non-navigable until #792 ships
- This is informational only; does not block the rest of the UI
