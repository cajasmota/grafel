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
  // Fix #1532-4: out-of-box defaults must not produce overlapping "blobs".
  // Lower base + gentler degree scale + a tight max multiplier so a high-degree
  // hub stays at most ~1.8× the base size (was 3× of a much larger base).
  //
  // Fix #1580: this is the REFERENCE base size for a small/mid graph. On a large
  // (≈19k-node) graph that fixed 90 is far too big — nodes overlap into colored
  // blobs when zoomed out. The graph-canvas now AUTO-SCALES this down inversely
  // with sqrt(nodeCount) (see baseSizeForCount), so the user-facing default knob
  // can stay readable on a small graph while a huge graph renders as fine points.
  baseSize: 90,
  degreeScale: 18,
  maxMultiplier: 1.8,
};

// Fix #1580: the base-size slider/clamp floor. Lowered from 40 to 4 so a very
// dense (≈19k-node) graph can be tuned down to fine points instead of blobs.
export const NODE_BASE_SIZE_MIN = 4;
export const NODE_BASE_SIZE_MAX = 320;

/**
 * Fix #1580: node-count-aware DEFAULT base size. A 19k-node graph needs a much
 * smaller base than a 200-node one, so scale the reference default DOWN inversely
 * with sqrt(nodeCount), normalized to a ~1.5k-node reference graph, and floor it
 * at NODE_BASE_SIZE_MIN. This is applied in graph-canvas to the EFFECTIVE base
 * size whenever the user hasn't overridden the knob, so a freshly-loaded huge
 * graph renders as points, not blobs, with no manual tuning.
 */
export function baseSizeForCount(count: number, reference = DEFAULT_NODE_SIZING.baseSize): number {
  if (!Number.isFinite(count) || count <= 0) return reference;
  const REF_NODES = 1500; // graph size the reference baseSize was tuned for
  const scaled = reference * Math.sqrt(REF_NODES / count);
  return Math.max(NODE_BASE_SIZE_MIN, Math.min(reference, scaled));
}

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
const DEFAULTS_VERSION = 4;
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
  setFocusRoot: (id: string | null) => void;
  setEgoHops: (hops: number) => void;
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

  enabledEdgeKinds: new Set(ALL_EDGE_KINDS),
  activeRepos: null,
  lod: "mid",

  colorMode: "repo",
  groupBy: "repo",
  groupingTouched: false,

  simulation: persistedJSON("ag.v2.graph.sim", DEFAULT_SIMULATION, STORED_DEFAULTS_VERSION),
  nodeSizing: persistedJSON("ag.v2.graph.sizing", DEFAULT_NODE_SIZING, STORED_DEFAULTS_VERSION),
  render: persistedJSON("ag.v2.graph.render", DEFAULT_RENDER, STORED_DEFAULTS_VERSION),

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
      // Fix #1580: clamp baseSize to the lowered floor so the knob can go to
      // single digits (fine points on a dense graph) but never below 4 / above max.
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
      focusRootId: null,
    }),
}));
