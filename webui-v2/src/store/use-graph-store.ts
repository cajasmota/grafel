/* ============================================================
   store/use-graph-store.ts — Graph-screen UI state (Zustand).

   Holds the per-screen interaction + tuning state: selection, hover,
   focus ego-graph, filters, group-by, and the four live tuning panels
   (Node Sizing / Simulation / Rendering / Group-by). The tuning knobs are
   persisted to localStorage (lesson ported from v1) so the owner's tuning
   survives reloads.

   The heavy graph DATA lives in TanStack Query (use-graph.ts); this store
   only holds UI/interaction state.
   ============================================================ */

import { create } from "zustand";
import type { EdgeKind } from "@/data/types";

export type ColorMode = "repo" | "module" | "community" | "degree";
export type GroupByMode = "repo" | "community" | "module" | "none";
export type LodLevel = "low" | "mid" | "high";

export interface SimulationConfig {
  linkSpring: number;
  linkDistance: number;
  friction: number;
  repulsion: number;
  center: number;
  /** ≤2s settle cap (the hard-won knob). Seconds. */
  settleTime: number;
}

export interface NodeSizingConfig {
  /**
   * Fix #1607: baseSize is now a USER MULTIPLIER (≈1.0 = "leave it auto"), NOT an
   * absolute pixel/size value. The graph-canvas computes an AUTOMATIC base size in
   * screen-pixels from the node count (big graph → smaller dots, small graph →
   * bigger nodes; see autoBasePx) and multiplies it by this knob. So 1.0 gives the
   * tuned auto size for any graph; 0.5 halves it, 2.0 doubles it — proportional on
   * every graph instead of an absolute that only suited one node count.
   */
  baseSize: number;
  /** log10(degree) multiplier — hub emphasis, capped by maxMultiplier. */
  degreeScale: number;
  maxMultiplier: number;
}

export interface RenderConfig {
  pointOpacity: number;
  pointSizeScale: number;
  scalePointsOnZoom: boolean;
  maxPointSize: number;
  linkWidthScale: number;
  linkOpacity: number;
  showLinks: boolean;
}

export const DEFAULT_SIMULATION: SimulationConfig = {
  // Fix #1562: the #1558 tuning (linkSpring 2.2 / repulsion 0.12 / center 1.6 /
  // gravity 0.35 + a mid-settle re-heat) was unstable on a LARGER graph: the
  // strong attractive forces (link-spring + center + gravity + cluster) vastly
  // outweighed the near-zero repulsion, so on the full 1316-node graph positions
  // collapsed and oscillated until they DIVERGED to NaN/Infinity. cosmos.gl then
  // tried to size a Float32Array from the Infinity-derived bounds and threw
  // "RangeError: Array buffer allocation failed". (At ~232 nodes the smaller
  // force sums stayed inside the stable basin so it never repro'd.)
  //
  // Re-tuned to a cohesive-but-STABLE balance: restore enough repulsion to
  // counter the attractive forces (nodes can't all pile onto one point), pull
  // link-spring and center back to sane values, and lower gravity. The graph
  // still coheres and fills the canvas (no hollow ring) but the net force stays
  // bounded so positions can never diverge. (was linkSpring 2.2 / repulsion 0.12
  // / center 1.6 / gravity 0.35 / settle 4.5)
  //
  // Fix #1566: now that the radial cluster ring + strong cluster force are gone
  // (see buildGroupCenters + simulationCluster), the link-spring and center are
  // the dominant placement forces — so raise them (BOUNDED) so connected
  // clusters pull adjacent and the mass fills the center instead of ringing.
  // Repulsion is kept ample so the stronger attraction can't collapse/diverge
  // at 1316 nodes (the #1562 stability invariant). (was linkSpring 1.3 /
  // center 0.9 / repulsion 0.7)
  linkSpring: 1.7,
  linkDistance: 12,
  friction: 0.88,
  repulsion: 0.85,
  center: 1.2,
  // A 3s settle is enough for this balance to converge while staying ≤ the 6s
  // hard cap. (Fix #1562)
  settleTime: 3.0,
};

export const DEFAULT_NODE_SIZING: NodeSizingConfig = {
  // Fix #1607: baseSize is a MULTIPLIER around the auto (count-derived) base, so
  // the out-of-box default is 1.0 ("use the computed auto size"). The auto base in
  // screen-pixels is computed by autoBasePx() from the node count, so 19k and 1.3k
  // graphs both get a sensible default with no manual tuning. degreeScale/maxMult
  // give hubs a gentle, hard-capped boost so no node ever blobs.
  baseSize: 1.0,
  degreeScale: 0.5,
  maxMultiplier: 2.2,
};

// Fix #1607: baseSize is now a unitless MULTIPLIER, so the slider runs 0.2..4×.
export const NODE_BASE_SIZE_MIN = 0.2;
export const NODE_BASE_SIZE_MAX = 4;

/**
 * Fix #1607: AUTOMATIC base node size in SCREEN PIXELS (at neutral zoom = 1),
 * derived purely from the node count. The whole point of this fix is that the
 * defaults look right on BOTH a 19,613-node graph and a 1,320-node graph without
 * touching any knob, so the base must adapt to density.
 *
 * Model: nodes share the viewport area, so per-node "breathing room" scales like
 * 1/sqrt(count). We anchor that to a comfortable on-screen diameter and clamp to a
 * perceptible-but-non-overlapping pixel band:
 *
 *   px = clamp( K / sqrt(count), MIN_PX, MAX_PX )
 *
 * Tuned so:
 *   • count ≈ 1,320  → ~9.5px   (small graph: clearly visible discs)
 *   • count ≈ 19,613 → ~2.8px   (huge graph: fine perceptible points, no blobs)
 *   • count ≈ 200    → capped at MAX_PX (tiny graph stays tasteful, not giant)
 *
 * This is the BASE at zoom=1; graph-canvas multiplies by the user baseSize knob
 * and then applies a SUBLINEAR, capped zoom response on top (see zoomSizeFactor).
 */
export const NODE_MIN_PX = 2.0;
export const NODE_MAX_PX = 14;
export function autoBasePx(count: number): number {
  if (!Number.isFinite(count) || count <= 0) return 8;
  const K = 350; // K/sqrt(count): 1320→9.6px, 19613→2.5px, 200→24.7px(→clamped)
  const px = K / Math.sqrt(count);
  return Math.max(NODE_MIN_PX, Math.min(NODE_MAX_PX, px));
}

export const DEFAULT_RENDER: RenderConfig = {
  pointOpacity: 0.92,
  // Fix #1607: the per-node SIZE buffer is now authored directly in screen-pixels
  // (autoBasePx × knobs), so the global scale is a neutral 1.0 — the px values map
  // 1:1 on screen at zoom=1. (Was 0.22, which only made sense against the old
  // ~90-unit absolute base.)
  pointSizeScale: 1.0,
  // Fix #1607: this flag now means "GROW nodes on zoom" and is driven by OUR
  // sublinear, px-capped updater (NOT cosmos's built-in linear law, which is
  // forced off in graph-canvas). DEFAULT ON: nodes grow gently (z^0.5) as you zoom
  // in so they read as discs — but the px cap stops them blobbing — and a floor
  // keeps them perceptible when zoomed out. OFF = constant on-screen pixel size.
  scalePointsOnZoom: true,
  // Fix #1607: this is now a REAL on-screen pixel CAP, enforced by the zoom-driven
  // size updater so the largest (hub) node can never blob past this many px no
  // matter how far the user zooms in. (Previously cosmos had no maxPointSize at
  // all, so this knob was inert — a root cause of the "big blobs zoomed in" bug.)
  maxPointSize: 26,
  // Fix #1558-1: the long cross-module links between islands vanished when
  // zoomed OUT. Raise the default width scale so even the thin same-repo links
  // clear the visible-pixel floor at every zoom level (see packLinkWidths,
  // which now also enforces a hard minimum on-screen width).
  linkWidthScale: 1.4,
  // Fix #1548-2: edges must read clearly from the FIRST paint (not just after
  // settle). Raise the default same-repo link opacity further so relationships
  // are visible immediately on a light background.
  linkOpacity: 0.6,
  showLinks: true,
};

// Structural edge kinds — default ON (the long-standing graph behaviour).
export const STRUCTURAL_EDGE_KINDS: EdgeKind[] = [
  "CALLS",
  "REFERENCES",
  "RENDERS",
  "DEPENDS_ON",
  "EXTENDS",
  "CONTAINS",
  "IMPORTS",
];

// Semantic edge kinds (#4252) — daemon-emitted, reach the payload unfiltered,
// but default OFF because they can be high-volume / noisy. Toggleable.
export const SEMANTIC_EDGE_KINDS: EdgeKind[] = [
  "INJECTED_INTO",
  "THROWS",
  "CATCHES",
  "JOINS_COLLECTION",
  "HTTP_ENDPOINT_CALL",
];

// Every selectable edge kind (structural + semantic).
export const ALL_EDGE_KINDS: EdgeKind[] = [...STRUCTURAL_EDGE_KINDS, ...SEMANTIC_EDGE_KINDS];

// Default-ON set: structural kinds only. Semantic kinds start hidden.
const DEFAULT_ENABLED_EDGE_KINDS: EdgeKind[] = STRUCTURAL_EDGE_KINDS;

// Fix #1567-4: persisted-defaults VERSION. The localStorage tuning keys
// (ag.v2.graph.sim/sizing/render) override new code defaults forever, so shipped
// default changes (e.g. #1569's + this PR's) never reach users who already have
// a stored value — a recurring papercut. Stamp a version alongside the stored
// tuning; on load, if the stored version is missing or < the current one, DISCARD
// the stale stored tuning and adopt the new code defaults. Bump this whenever a
// default in DEFAULT_SIMULATION / DEFAULT_NODE_SIZING / DEFAULT_RENDER changes so
// the change actually lands on next load with no manual Reset.
// Fix #1580: bump so the lowered base-size floor + auto-scale defaults reach
// users who already have a stored sizing blob (e.g. baseSize 90 from v2).
// Fix #1581: bump again — the recurring "reload restores a contracted layout"
// bug is rooted in stale CACHED LAYOUTS produced by retired force defaults. We
// scope the layout cache by graph-layout-cache.LAYOUT_VERSION (kept in lock-step
// with this constant); bumping here also discards stale persisted TUNING so the
// current force defaults (which produce the good spread) actually apply on load.
// Fix #1607: bump to 5 — the sizing model changed shape entirely (baseSize is now
// a unitless multiplier, pointSizeScale is 1.0, scalePointsOnZoom is off, defaults
// retuned). A stored v4 sizing/render blob (baseSize 90, pointSizeScale 0.22,
// scalePointsOnZoom true) would be catastrophically wrong under the new model, so
// discard it and adopt the new code defaults on next load — no manual Reset.
const DEFAULTS_VERSION = 5;
const VERSION_KEY = "ag.v2.graph.defaultsVersion";

function readStoredVersion(): number {
  if (typeof localStorage === "undefined") return DEFAULTS_VERSION;
  const raw = localStorage.getItem(VERSION_KEY);
  const v = raw == null ? 0 : Number.parseInt(raw, 10);
  return Number.isFinite(v) ? v : 0;
}

/**
 * Fix #1567-4: load a persisted tuning blob ONLY if the stored defaults version
 * is current. On a stale (or missing) version we ignore localStorage entirely and
 * return the code default, so new shipped defaults apply on next load. The version
 * stamp is (re)written by migrateDefaults() once at module init.
 */
function persistedJSON<T>(key: string, fallback: T, storedVersion: number): T {
  if (typeof localStorage === "undefined") return fallback;
  if (storedVersion < DEFAULTS_VERSION) return fallback; // discard stale tuning
  try {
    const raw = localStorage.getItem(key);
    return raw ? ({ ...fallback, ...JSON.parse(raw) } as T) : fallback;
  } catch {
    return fallback;
  }
}

/**
 * Fix #1567-4: on a stale stored version, clear the old tuning keys and stamp the
 * current version, so subsequent setSimulation/etc. persist fresh. Returns the
 * version that was on disk at load time (used to gate persistedJSON above).
 */
function migrateDefaults(): number {
  const stored = readStoredVersion();
  if (typeof localStorage === "undefined") return stored;
  if (stored < DEFAULTS_VERSION) {
    try {
      localStorage.removeItem("ag.v2.graph.sim");
      localStorage.removeItem("ag.v2.graph.sizing");
      localStorage.removeItem("ag.v2.graph.render");
      localStorage.setItem(VERSION_KEY, String(DEFAULTS_VERSION));
    } catch {
      /* ignore quota / private-mode */
    }
  }
  return stored;
}

function persist<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* ignore */
  }
}

interface GraphState {
  // Interaction
  selectedNodeId: string | null;
  hoveredNodeId: string | null;
  search: string;
  focusedCommunityId: number | null;
  /** N-hop ego-graph focus: explicit node ids, or null for the full graph. */
  focusNodeIds: Set<string> | null;
  /** The node the ego sub-graph is rooted at (so the hops slider can re-run
   *  the BFS at a new depth). null when not in focus. (Fix #1564-4) */
  focusRootId: string | null;
  /** BFS depth for the ego sub-graph; the focus-view hops slider. (Fix #1564-4) */
  egoHops: number;
  filtersOpen: boolean;
  communitiesOpen: boolean;

  // Filters
  enabledEdgeKinds: Set<EdgeKind>;
  activeRepos: Set<string> | null; // null = all
  lod: LodLevel;
  /**
   * #4467 — minimum RENDERED degree a node must have to be shown. Computed from
   * the currently rendered edge set (after the edge-kind filter), so it respects
   * the user's edge-kind toggles and stays honest. 0 = show everything (default),
   * 1 = hide true zero-edge orphans, 2 = also hide degree-1 leaf nodes. Hiding is
   * always surfaced in the low-degree badge so it's explicit + reversible. This
   * DE-EMPHASIS/hide is purely a readability aid; the underlying missing-edge
   * extraction bugs are fixed separately.
   */
  minDegree: number;

  // View knobs
  colorMode: ColorMode;
  groupBy: GroupByMode;
  /**
   * Whether the user has explicitly chosen a color/group mode this session.
   * Until then, the screen is free to pick monorepo-aware defaults once the
   * graph data arrives (Fix #1532-1). Set true by setColorMode/setGroupBy.
   */
  groupingTouched: boolean;

  // Tuning (persisted)
  simulation: SimulationConfig;
  nodeSizing: NodeSizingConfig;
  render: RenderConfig;

  // #1386 — Module overview mode (closes epic #1380 alongside #1384).
  // When ON, the Graph screen renders a COLLAPSED view: one node per
  // module (sized by entity count / PageRank), edges = weighted
  // module→module aggregates, cycles (SCCs) highlighted as a group.
  // Clicking a module sets `expandedModule` and pops back into the
  // entity-level view filtered to that module's entities. Default OFF —
  // this is a separate mode, NOT a replacement for the Repo/Module/
  // Community/Degree color modes (which apply to the entity-level view).
  moduleOverviewMode: boolean;
  /** When non-null, the entity-level view is filtered to the entities of
   *  this `${repo}::${moduleName}` pair (the user "expanded" a module).
   *  Cleared by exiting via the focus banner. */
  expandedModule: { repo: string; moduleName: string } | null;

  // Re-layout request flag (flips true to force a fresh settle)
  relayoutNonce: number;

  // Actions
  setSelectedNode: (id: string | null) => void;
  setHoveredNode: (id: string | null) => void;
  setSearch: (q: string) => void;
  setFocusedCommunity: (id: number | null) => void;
  setFocusNodes: (ids: Set<string> | null) => void;
  setFocusRoot: (id: string | null) => void;
  setEgoHops: (hops: number) => void;
  setFiltersOpen: (open: boolean) => void;
  setCommunitiesOpen: (open: boolean) => void;
  toggleEdgeKind: (kind: EdgeKind) => void;
  toggleRepo: (repo: string) => void;
  clearRepos: () => void;
  setLod: (lod: LodLevel) => void;
  /** #4467 — set the min-degree threshold (clamped to 0..2). */
  setMinDegree: (d: number) => void;
  setColorMode: (m: ColorMode) => void;
  setGroupBy: (m: GroupByMode) => void;
  /**
   * Pick monorepo-aware default coloring/grouping ONCE per group, before the
   * user touches the controls. A monorepo (one repo, many modules) defaults to
   * per-MODULE color + module grouping (repo coloring would be one flat color);
   * a multi-repo group keeps Repo. (Fix #1532-1)
   */
  applyMonorepoDefaults: (isMonorepo: boolean) => void;
  setSimulation: (patch: Partial<SimulationConfig>) => void;
  setNodeSizing: (patch: Partial<NodeSizingConfig>) => void;
  setRender: (patch: Partial<RenderConfig>) => void;
  requestRelayout: () => void;
  clearAllFilters: () => void;
  resetView: () => void;

  // #1386 — Module overview actions.
  setModuleOverviewMode: (on: boolean) => void;
  setExpandedModule: (m: { repo: string; moduleName: string } | null) => void;
}

// Fix #1567-4: run the migration ONCE at module init (before the store reads any
// persisted tuning). Captures the on-disk version (to gate persistedJSON) and
// clears stale keys + stamps the current version as a side effect.
const STORED_DEFAULTS_VERSION = migrateDefaults();

export const useGraphStore = create<GraphState>((set) => ({
  selectedNodeId: null,
  hoveredNodeId: null,
  search: "",
  focusedCommunityId: null,
  focusNodeIds: null,
  focusRootId: null,
  egoHops: 3,
  filtersOpen: false,
  communitiesOpen: false,

  enabledEdgeKinds: new Set(DEFAULT_ENABLED_EDGE_KINDS),
  activeRepos: null,
  // Fix #1599: DEFAULT to HIGH so the FULL graph renders out of the box (no
  // node/edge thinning). MID/overview previously capped the daemon payload to
  // ~3000 nodes, which silently dropped most of a large graph (upvate: 3000 of
  // 19.6k nodes) AND nearly all cross-repo edges (10 of 376) — so the inter-repo
  // structure the emphasis tier exists to surface was never even served. HIGH
  // serves the complete corpus (19,613 nodes / 37,418 edges on upvate); cosmos.gl
  // renders it fine (verified #1562/#1580). MID/LOW stay selectable as lighter
  // tiers via the LOD control.
  lod: "high",

  // #4467 — show everything by default; low-degree nodes are de-emphasized
  // (smaller + dimmer) rather than hidden, so the default view is honest.
  minDegree: 0,

  colorMode: "repo",
  groupBy: "repo",
  groupingTouched: false,

  simulation: persistedJSON("ag.v2.graph.sim", DEFAULT_SIMULATION, STORED_DEFAULTS_VERSION),
  nodeSizing: persistedJSON("ag.v2.graph.sizing", DEFAULT_NODE_SIZING, STORED_DEFAULTS_VERSION),
  render: persistedJSON("ag.v2.graph.render", DEFAULT_RENDER, STORED_DEFAULTS_VERSION),

  // #1386 — module-overview defaults OFF; expandedModule null.
  moduleOverviewMode: false,
  expandedModule: null,

  relayoutNonce: 0,

  setSelectedNode: (selectedNodeId) => set({ selectedNodeId }),
  setHoveredNode: (hoveredNodeId) => set({ hoveredNodeId }),
  setSearch: (search) => set({ search }),
  setFocusedCommunity: (focusedCommunityId) => set({ focusedCommunityId }),
  setFocusNodes: (focusNodeIds) => set({ focusNodeIds }),
  setFocusRoot: (focusRootId) => set({ focusRootId }),
  setEgoHops: (egoHops) => set({ egoHops: Math.max(1, Math.min(6, Math.round(egoHops))) }),
  setFiltersOpen: (filtersOpen) => set({ filtersOpen }),
  setCommunitiesOpen: (communitiesOpen) => set({ communitiesOpen }),

  toggleEdgeKind: (kind) =>
    set((s) => {
      const next = new Set(s.enabledEdgeKinds);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      return { enabledEdgeKinds: next };
    }),
  toggleRepo: (repo) =>
    set((s) => {
      const next = new Set(s.activeRepos ?? []);
      if (next.has(repo)) next.delete(repo);
      else next.add(repo);
      return { activeRepos: next.size === 0 ? null : next };
    }),
  clearRepos: () => set({ activeRepos: null }),
  setLod: (lod) => set({ lod }),
  setMinDegree: (d) => set({ minDegree: Math.max(0, Math.min(2, Math.round(d))) }),
  setColorMode: (colorMode) => set({ colorMode, groupingTouched: true }),
  setGroupBy: (groupBy) => set({ groupBy, groupingTouched: true }),
  applyMonorepoDefaults: (isMonorepo) =>
    set((s) => {
      if (s.groupingTouched) return {}; // never override an explicit choice
      return isMonorepo
        ? { colorMode: "module" as ColorMode, groupBy: "module" as GroupByMode }
        : { colorMode: "repo" as ColorMode, groupBy: "repo" as GroupByMode };
    }),

  setSimulation: (patch) =>
    set((s) => {
      const simulation = { ...s.simulation, ...patch };
      persist("ag.v2.graph.sim", simulation);
      return { simulation };
    }),
  setNodeSizing: (patch) =>
    set((s) => {
      const nodeSizing = { ...s.nodeSizing, ...patch };
      // Fix #1607: clamp the baseSize MULTIPLIER to 0.2..4× so the knob nudges the
      // auto (count-derived) base proportionally without ever going to zero/absurd.
      if (patch.baseSize !== undefined) {
        nodeSizing.baseSize = Math.max(
          NODE_BASE_SIZE_MIN,
          Math.min(NODE_BASE_SIZE_MAX, nodeSizing.baseSize),
        );
      }
      persist("ag.v2.graph.sizing", nodeSizing);
      return { nodeSizing };
    }),
  setRender: (patch) =>
    set((s) => {
      const render = { ...s.render, ...patch };
      persist("ag.v2.graph.render", render);
      return { render };
    }),

  requestRelayout: () => set((s) => ({ relayoutNonce: s.relayoutNonce + 1 })),
  clearAllFilters: () =>
    set({
      enabledEdgeKinds: new Set(DEFAULT_ENABLED_EDGE_KINDS),
      activeRepos: null,
      // Fix #1599: clearing filters returns to the full-graph default (HIGH).
      lod: "high",
      // #4467 — clearing filters returns to "show everything".
      minDegree: 0,
    }),
  resetView: () =>
    set({
      selectedNodeId: null,
      hoveredNodeId: null,
      focusedCommunityId: null,
      focusNodeIds: null,
      focusRootId: null,
      // #1386 — Reset also exits the module-overview / expanded-module state so
      // the user lands back on the canonical full entity graph.
      moduleOverviewMode: false,
      expandedModule: null,
    }),

  // #1386 — toggling the overview mode clears any in-flight focus state so the
  // module-collapsed canvas renders cleanly; exiting also clears any expansion.
  setModuleOverviewMode: (moduleOverviewMode) =>
    set({
      moduleOverviewMode,
      expandedModule: null,
      selectedNodeId: null,
      focusNodeIds: null,
      focusRootId: null,
    }),
  setExpandedModule: (expandedModule) => set({ expandedModule }),
}));
