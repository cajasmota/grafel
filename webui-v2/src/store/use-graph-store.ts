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
  baseSize: number;
  /** log10(degree) multiplier. */
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
  // Fix #1558-2: the graph showed a HOLLOW RING of islands with an empty middle —
  // the cluster force pinned each module to a wide ring while the cross-module
  // links were too weak to pull connected modules together. Raise link-spring so
  // edges (esp. the 36 cross-module bridges) contract, raise center gravity so
  // the whole graph collapses inward and FILLS the canvas, and drop repulsion so
  // unconnected islands stop shoving each other to the rim. Balanced — clusters
  // still read as clusters, the graph is not a single blob.
  // (was linkSpring 1.0 / repulsion 1.0 / center 0.55)
  linkSpring: 2.2,
  linkDistance: 8,
  friction: 0.85,
  repulsion: 0.12,
  center: 1.6,
  // A slightly longer settle (still ≤ the 6s hard cap) lets the strong center +
  // gravity converge from the seed into the canvas-filling mass — the 2s cap
  // stopped it while the islands were still spread. (Fix #1558-2)
  settleTime: 4.5,
};

export const DEFAULT_NODE_SIZING: NodeSizingConfig = {
  // Fix #1532-4: out-of-box defaults must not produce overlapping "blobs".
  // Lower base + gentler degree scale + a tight max multiplier so a high-degree
  // hub stays at most ~1.8× the base size (was 3× of a much larger base).
  baseSize: 90,
  degreeScale: 18,
  maxMultiplier: 1.8,
};

export const DEFAULT_RENDER: RenderConfig = {
  pointOpacity: 0.92,
  pointSizeScale: 0.22,
  scalePointsOnZoom: true,
  // Fix #1532-4: cap the on-screen pixel size so no node becomes a giant blob
  // even when zoomed in (was 60).
  maxPointSize: 34,
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

const ALL_EDGE_KINDS: EdgeKind[] = [
  "CALLS",
  "REFERENCES",
  "RENDERS",
  "DEPENDS_ON",
  "EXTENDS",
  "CONTAINS",
  "IMPORTS",
];

function persistedJSON<T>(key: string, fallback: T): T {
  if (typeof localStorage === "undefined") return fallback;
  try {
    const raw = localStorage.getItem(key);
    return raw ? ({ ...fallback, ...JSON.parse(raw) } as T) : fallback;
  } catch {
    return fallback;
  }
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
  filtersOpen: boolean;
  communitiesOpen: boolean;

  // Filters
  enabledEdgeKinds: Set<EdgeKind>;
  activeRepos: Set<string> | null; // null = all
  lod: LodLevel;

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

  // Re-layout request flag (flips true to force a fresh settle)
  relayoutNonce: number;

  // Actions
  setSelectedNode: (id: string | null) => void;
  setHoveredNode: (id: string | null) => void;
  setSearch: (q: string) => void;
  setFocusedCommunity: (id: number | null) => void;
  setFocusNodes: (ids: Set<string> | null) => void;
  setFiltersOpen: (open: boolean) => void;
  setCommunitiesOpen: (open: boolean) => void;
  toggleEdgeKind: (kind: EdgeKind) => void;
  toggleRepo: (repo: string) => void;
  clearRepos: () => void;
  setLod: (lod: LodLevel) => void;
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
}

export const useGraphStore = create<GraphState>((set) => ({
  selectedNodeId: null,
  hoveredNodeId: null,
  search: "",
  focusedCommunityId: null,
  focusNodeIds: null,
  filtersOpen: false,
  communitiesOpen: false,

  enabledEdgeKinds: new Set(ALL_EDGE_KINDS),
  activeRepos: null,
  lod: "mid",

  colorMode: "repo",
  groupBy: "repo",
  groupingTouched: false,

  simulation: persistedJSON("ag.v2.graph.sim", DEFAULT_SIMULATION),
  nodeSizing: persistedJSON("ag.v2.graph.sizing", DEFAULT_NODE_SIZING),
  render: persistedJSON("ag.v2.graph.render", DEFAULT_RENDER),

  relayoutNonce: 0,

  setSelectedNode: (selectedNodeId) => set({ selectedNodeId }),
  setHoveredNode: (hoveredNodeId) => set({ hoveredNodeId }),
  setSearch: (search) => set({ search }),
  setFocusedCommunity: (focusedCommunityId) => set({ focusedCommunityId }),
  setFocusNodes: (focusNodeIds) => set({ focusNodeIds }),
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
      enabledEdgeKinds: new Set(ALL_EDGE_KINDS),
      activeRepos: null,
      lod: "mid",
    }),
  resetView: () =>
    set({
      selectedNodeId: null,
      hoveredNodeId: null,
      focusedCommunityId: null,
      focusNodeIds: null,
    }),
}));
