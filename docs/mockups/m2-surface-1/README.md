# Surface 1 — Graph Viewer: Mockup Package

**Sprint:** Milestone 2
**Date:** 2026-05-20
**Status:** Design complete, pending frontend implementation

---

## Files in this directory

| File | Description |
|---|---|
| `graph-zoom-out.png` | View 1: Full zoomed-out, 25k-node graph, 6 community centroids |
| `graph-mid-zoom.png` | View 2: Mid-zoom showing Core Services community + god-nodes |
| `graph-inspector.png` | View 3: Full zoom on Auth cluster, selected node, inspector panel |
| `graph-states.png` | Loading / Empty / Error states + 2D fallback mode |
| `mockup-graph-zoom-out.svg` | Source SVG — editable |
| `mockup-graph-mid-zoom.svg` | Source SVG — editable |
| `mockup-graph-inspector.svg` | Source SVG — editable |
| `mockup-graph-states.svg` | Source SVG — editable |

---

## React component → plan section mapping

| Visual element | React component (§4) | File location |
|---|---|---|
| 3D canvas WebGL | `<GraphCanvas3D>` | `components/graph/GraphCanvas3D.tsx` |
| 2D fallback canvas | `<GraphCanvas2D>` | `components/graph/GraphCanvas2D.tsx` |
| Edge kind chip row | `<EdgeKindFilters>` | `components/graph/EdgeKindFilters.tsx` |
| Community color key | `<CommunityLegend>` | `components/graph/CommunityLegend.tsx` |
| Search input | `<GraphSearchTypeahead>` | `components/graph/GraphSearchTypeahead.tsx` |
| Search input bar + layout toggle | `<GraphToolbar>` | `components/graph/GraphToolbar.tsx` |
| Selected node metadata panel | `<EntityInspector>` | `components/graph/EntityInspector.tsx` |
| Amber pulsing halo ring on god-nodes | `<GodNodeHalo>` | `components/graph/GodNodeHalo.tsx` |
| Kind icon in inspector neighbor list | `<NodeChip>` | `components/graph/NodeChip.tsx` |
| Edge kind chip in inspector edges list | `<EdgeBadge>` | `components/graph/EdgeBadge.tsx` |
| LoD zoom bar (annotation UI) | `useGraphData` derived state | `hooks/useGraphData.ts` |

**Total atomic components: 11**

---

## LoD strategy (mapped to views)

| View | LoD mode | Trigger threshold | Canvas content |
|---|---|---|---|
| View 1 (zoom-out) | `CENTROIDS` | >5,000 visible nodes | Community centroids only, sized by member count |
| View 2 (mid-zoom) | `MID` | 1,000–5,000 visible nodes | Centroids + top-K god-nodes (PageRank) radiating as spokes |
| View 3 (full-zoom) | `FULL` | <1,000 visible nodes | All entity nodes within frustum |
| Any | Override | Node selected | Selected + 1-hop neighbors always visible regardless of LoD |

---

## Hook contracts

| Hook | Responsibility |
|---|---|
| `useGraphData(group, lodLevel, filters)` | Fetches `GET /api/graph/{group}?lod=centroids\|mid\|full`; derives visible arrays |
| `useEntityInspector(entityId)` | Fetches `/api/inspect?id=` + `/api/expand?node_id=`; drives `<EntityInspector>` |
| `useEdgeKindFilters()` | Reads/writes URL param `edge_kinds`; returns toggle handler |
| `useCommunityColors(communities)` | Derives stable `Map<communityId, hex>` from seeded palette |
| `useGraphCamera()` | Zustand slice: `zoomToNode`, `resetView`, `cameraRef` |
| `useGraphSelection()` | URL param `entity`; syncs inspector open state |
| `useGodNodes(groupId)` | Fetches `/api/groups/{group}/god-nodes`; returns `Set<entityId>` |
| `useGraphSearch(q)` | Delegates to shared `useSearchQuery(q)` |

---

## Interaction notes

### Zooming (LoD transitions)
- Scroll wheel / pinch → zoom level changes
- **[ANIM]** LoD crossfade: 400ms ease when crossing 5k/1k thresholds; non-WebGL elements fade-in
- LoD indicator badge in toolbar updates immediately

### Selecting a node
- Click any entity node → `useGraphSelection().select(id)` updates URL param `?entity=<id>`
- **[ANIM]** Non-1-hop edges dim to 0.12 opacity, 200ms ease-in
- **[ANIM]** `<EntityInspector>` slides in from right: `translateX(280px)→translateX(0)`, 280ms ease-out
- **[ANIM]** Selected node: green pulsing ring, scale 1.0→1.3, opacity 1→0, 2s loop; prefers-reduced-motion: static ring

### God-node halos
- **[ANIM]** Amber ring: scale 1.0→1.4, opacity 0.5→0, 2s ease-out loop
- Enabled only in MID and FULL LoD modes

### Community centroid click (CENTROID mode)
- Click centroid → camera zooms into that community (transitions to MID LoD)
- **[ANIM]** Camera fly-to: 800ms ease-in-out, via `useGraphCamera().zoomToNode(centroidId)`

### Edge kind filter chip
- Click chip → toggle that edge kind on/off
- **[ANIM]** Active chip: tinted bg transition 100ms; canvas edges fade-in/out 200ms

### "Open in Flows" button (inspector)
- Navigates to `/flows?entity=<id>` — process flows scoped to selected entity

### 2D fallback
- Triggered by `prefers-reduced-motion: reduce` media query OR explicit 2D toggle in toolbar
- d3-force simulation replaces WebGL; no CSS animation; same layout; god-nodes shown with amber stroke ring

---

## Aesthetic decisions

Follows Surface 4 baseline exactly:
- **Dark mode default:** `#060912` canvas bg; `#0C1118` sidebar; `#141920` elevated surfaces
- **Typography:** Inter (UI) + JetBrains Mono (entity names, file paths, IDs)
- **Brand accent:** `#22C55E` — active nav item, selected node ring, sidebar active indicator
- **Community colors:** 6-color seeded palette (cyan, green, purple, amber, blue, rose) — deterministic per community ID, not random
- **Canvas:** WebGL via `3d-force-graph`; deep space atmosphere with depth fog overlay

---

## Spec ambiguities

1. **Centroid position algorithm not specified.** The LoD aggregator computes centroid positions server-side — it should return `x, y, z` in normalized 3d-force-graph space. The algorithm (mass-centroid vs PageRank-weighted vs geometric) is not defined in the plan. Recommendation: PageRank-weighted centroid (nodes with higher centrality pull the center). Backend team to confirm.

2. **God-node count K is unspecified.** The plan says "top-K by centrality" but doesn't define K. Design shows 7-8 god-nodes per community. Recommendation: K=8 per community in MID mode; K=∞ (all) in FULL mode. Backend to expose as a config parameter.

3. **"Always show selected + 1-hop regardless of LoD" requires server awareness.** When the selected entity is out of the current LoD frustum (e.g., in CENTROID mode, a specific entity is selected via deep link), the server must return that entity's node + its 1-hop neighbors in addition to the LoD shape. The REST contract needs a `?pin_entity=<id>` param or equivalent. Not currently in Section 3.

4. **`useGraphCamera` Zustand state and 3d-force-graph camera sync.** The plan mentions `cameraRef` in the Zustand store but doesn't specify how camera position is serialized to URL for deep-linking. Current design stores `?entity=<id>` (selection only) not camera position. Camera teleport on deep-link should use `zoomToNode(id)` which sets camera, not URL params. This avoids giant URL state.

5. **2D canvas uses d3-force or 3d-force-graph in 2D mode?** The plan says `<GraphCanvas2D>` has "same prop interface as 3D." Option A: use `3d-force-graph` in `numDimensions=2` mode (simpler code). Option B: use d3-force directly (lighter). Recommendation: Option A for code parity; Surface 3 uses d3-force directly anyway.

---

*Designed by BB-8 (Design Lead) · 2026-05-20*
