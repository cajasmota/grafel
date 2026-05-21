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

export type ColorMode = "repo" | "community" | "degree";
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
  linkSpring: 1.0,
  linkDistance: 8,
  friction: 0.85,
  repulsion: 1.2,
  center: 0.4,
  settleTime: 2.0,
};

export const DEFAULT_NODE_SIZING: NodeSizingConfig = {
  baseSize: 120,
  degreeScale: 30,
  maxMultiplier: 3.0,
};

export const DEFAULT_RENDER: RenderConfig = {
  pointOpacity: 0.92,
  pointSizeScale: 0.22,
  scalePointsOnZoom: true,
  maxPointSize: 60,
  linkWidthScale: 1.0,
  linkOpacity: 0.18,
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
  setColorMode: (colorMode) => set({ colorMode }),
  setGroupBy: (groupBy) => set({ groupBy }),

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
