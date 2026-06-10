/* ============================================================
   routes/graph.tsx — the Graph screen (WebUI v2 hero surface).

   Clean layered composition:
     api client (lib/api) → data hook (use-graph) → this screen + the
     cosmos.gl canvas component (components/graph/graph-canvas).

   The screen owns the toolbar (search / communities / filters / reset /
   group-by + color mode), the WebGL canvas, the floating inspector, the
   filters drawer (+ tuning panels), and the LOD badge. All interaction +
   tuning state lives in use-graph-store; the heavy graph data in TanStack
   Query.
   ============================================================ */

import { useCallback, useEffect, useMemo, useRef, useState, lazy, Suspense } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { X, RotateCcw, SlidersHorizontal, Boxes } from "lucide-react";
import { SearchInput, Pill, Kbd, ScreenDescription, AgentUsage } from "@/components/ui";
import { useGraph } from "@/hooks/use-graph";
import { useModuleAnalysis } from "@/hooks/use-module-analysis";
import {
  useGraphStore,
  type ColorMode,
  ALL_EDGE_KINDS,
  STRUCTURAL_EDGE_KINDS,
} from "@/store/use-graph-store";
import { useAppStore } from "@/store/use-app-store";
import type { EdgeKind, GraphNode } from "@/data/types";
import { ModuleOverview } from "@/components/graph/module-overview";
const GraphCanvas = lazy(() =>
  import("@/components/graph/graph-canvas").then((m) => ({ default: m.GraphCanvas })),
);
import type { GraphCanvasHandle } from "@/components/graph/graph-canvas";
import { NodeInspector } from "@/components/graph/node-inspector";
import { FiltersDrawer } from "@/components/graph/filters-drawer";
import { CommunitiesPopover } from "@/components/graph/communities-popover";
import { MCPActivityOverlay } from "@/components/graph/mcp-activity-overlay";
import { GraphJarvisOverlay } from "@/components/graph/graph-jarvis-overlay";
import { useGraphHighlight } from "@/hooks/use-graph-highlight";
import { useGraphJarvisReplay } from "@/hooks/use-graph-jarvis-replay";

/**
 * #1386 — derive the entity-level "module key" from a node's source file.
 *
 * Mirrors `moduleKey` in graph-canvas.tsx (last 2 path segments minus the
 * file part). Kept here in addition to (not instead of) the canvas copy so
 * the route can filter the entity set when a user expands a module from
 * the overview, without reaching into the canvas internals.
 */
function moduleKeyOf(sourceFile: string): string {
  if (!sourceFile) return "";
  const parts = sourceFile.replace(/\\/g, "/").split("/");
  return parts.slice(0, -1).slice(-2).join("/");
}

const COLOR_MODES: { id: ColorMode; label: string }[] = [
  { id: "repo", label: "Repo" },
  { id: "module", label: "Module" },
  { id: "community", label: "Community" },
  { id: "degree", label: "Degree" },
];

/**
 * #4319 — SUPER-HUB DEGREE CAP (graph-view freeze fix).
 *
 * A single structural aggregate node can be incident to a huge share of the
 * graph — e.g. the `core-backend-v3` Module is connected to ~every entity via
 * CONTAINS (degree ~15,613 on the upvate-v3 graph), and new aggregate
 * "god-nodes" (AggregationBuilder, betweenness ~141k) form similar mega-stars.
 *
 * cosmos.gl's force-directed layout does O(edges) work PER TICK (the link
 * spring is evaluated for every link every frame), and a single node with tens
 * of thousands of incident edges both (a) dominates that per-tick cost and (b)
 * creates an extreme force imbalance that makes the layout thrash and never
 * settle — pinning the main thread and FREEZING the page.
 *
 * Such a node is also visually useless: a 15k-edge hairball star conveys no
 * structure. So we EXCLUDE any node whose incident-edge count (in the currently
 * rendered edge set) exceeds HUB_DEGREE_CAP, and drop its incident edges, before
 * the data ever reaches the cosmos canvas. This is a pure pre-render filter:
 *
 *   • Normal graphs are untouched — no real entity approaches ~2k incident
 *     edges; the cap only ever trips on pathological structural aggregates.
 *   • The count of pruned hubs is surfaced in the LOD badge so the omission is
 *     visible and explainable, never silent.
 *   • The canvas stays a pure renderer (it caps nothing itself); the transform
 *     lives here alongside the existing edge-kind filter.
 *
 * The cap is generous (well above any sane real degree) so it degrades ONLY the
 * pathological mega-stars and never a legitimately well-connected hub.
 */
const HUB_DEGREE_CAP = 2000;

export default function GraphScreen() {
  const { groupId = "demo" } = useParams();
  const theme = useAppStore((st) => st.theme);
  const isDark = theme === "dark";

  const s = useGraphStore();
  const [searchParams, setSearchParams] = useSearchParams();
  const { data, isLoading, isError } = useGraph(groupId, { lod: s.lod });

  // #1386 — module-level GDS for the "Module overview" mode. Lazy: only
  // fetched while the overview toggle is ON, so the default graph route has
  // zero extra network cost.
  const moduleAnalysis = useModuleAnalysis(groupId, {
    enabled: s.moduleOverviewMode,
  });

  const searchRef = useRef<HTMLInputElement>(null);
  const canvasRef = useRef<GraphCanvasHandle | null>(null);

  // #1157 Jarvis: subscribe to the MCP activity SSE stream and derive the
  // transient glow set + epoch the canvas animates. Default ON.
  const jarvis = useGraphHighlight();

  // #1932 JARVIS overhaul: the SVG overlay (chevrons + comet + trail) lives in
  // its own component and reads node positions through the canvas's imperative
  // handle. The lazy GraphCanvas only resolves AFTER Suspense, so we bump this
  // tick state once the ref attaches so the overlay re-renders with a live
  // handle. (Refs alone don't trigger re-renders.)
  const [canvasReady, setCanvasReady] = useState(0);
  const setCanvasRef = useCallback((h: GraphCanvasHandle | null) => {
    canvasRef.current = h;
    if (h) setCanvasReady((v) => v + 1);
  }, []);

  // #1932 JARVIS replay controller. Re-uses the existing per-entry glow path
  // (jarvis.replay) for the daemon-side highlight and adds the comet / pulse /
  // scrubber on top via the shared FlowAnim engine.
  const isBridgeEdgeProxy = useCallback((src: string, tgt: string) => {
    return canvasRef.current?.isBridgeEdge(src, tgt) ?? false;
  }, []);
  const replay = useGraphJarvisReplay({
    eventLog: jarvis.eventLog,
    onReplayEvent: jarvis.replay,
    isBridgeEdge: isBridgeEdgeProxy,
  });
  // Pass the live handle through a render-dep variable. canvasReady bumps when
  // setCanvasRef attaches, ensuring the overlay receives the actual ref value
  // (not the initial null) once mount completes.
  const liveCanvasHandle = canvasReady > 0 ? canvasRef.current : null;

  // ── ?node= deep-link: restore on mount, persist on selection change ──────────
  // On first render, if the URL carries ?node=<id>, apply it as the selected
  // node. focusEgo is called in a separate data-ready effect below.
  useEffect(() => {
    const nodeParam = searchParams.get("node");
    if (nodeParam && !s.selectedNodeId) {
      s.setSelectedNode(nodeParam);
    }
    // Only run on mount — searchParams intentionally excluded to avoid loops.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Mirror selectedNodeId → URL so a deep-link can be copied.
  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (s.selectedNodeId) {
          next.set("node", s.selectedNodeId);
        } else {
          next.delete("node");
        }
        return next;
      },
      { replace: true },
    );
  }, [s.selectedNodeId, setSearchParams]);

  // ── keyboard: / focus search, F filters, Escape cascade ─────────────────────
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      const typing = tag === "INPUT" || tag === "TEXTAREA";
      if (e.key === "/" && !typing) {
        e.preventDefault();
        searchRef.current?.focus();
      } else if (e.key === "f" && !typing) {
        e.preventDefault();
        s.setFiltersOpen(!s.filtersOpen);
      } else if (e.key === "Escape") {
        if (s.filtersOpen) s.setFiltersOpen(false);
        else if (s.communitiesOpen) s.setCommunitiesOpen(false);
        else if (s.focusNodeIds) exitFocus();
        else if (s.selectedNodeId) s.setSelectedNode(null);
        else if (s.focusedCommunityId != null) s.setFocusedCommunity(null);
        // #1386 — last-resort Escape: exit Module overview mode.
        else if (s.moduleOverviewMode) s.setModuleOverviewMode(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [s]);

  // Edge-kind filter applied client-side (kinds are cheap to filter locally).
  const kindFilteredEdges = useMemo(() => {
    if (!data) return [];
    const edges = data.edges ?? [];
    // Fast path: every selectable kind enabled → no filtering needed.
    if (s.enabledEdgeKinds.size === ALL_EDGE_KINDS.length) return edges;
    return edges.filter((e) => s.enabledEdgeKinds.has(e.kind as EdgeKind));
  }, [data, s.enabledEdgeKinds]);

  const allNodes = data?.nodes ?? [];

  // ── #4319 — prune pathological super-hub nodes (see HUB_DEGREE_CAP) ───────────
  // Count each node's incident edges in the CURRENTLY RENDERED edge set (after
  // the edge-kind filter), then drop any node above the cap together with its
  // incident edges, so cosmos.gl never sees a 15k-edge star that freezes the
  // layout. Computed from the rendered edges (not the daemon `degree`) so it
  // respects the user's edge-kind toggles. No-op on normal graphs.
  const { nodes, edges, prunedHubCount, lowDegreeCount, orphanCount, hiddenLowDegreeCount } =
    useMemo(() => {
      const incident = new Map<string, number>();
      for (const e of kindFilteredEdges) {
        incident.set(e.source, (incident.get(e.source) ?? 0) + 1);
        incident.set(e.target, (incident.get(e.target) ?? 0) + 1);
      }
      const prunedIds = new Set<string>();
      for (const n of allNodes) {
        if ((incident.get(n.id) ?? 0) > HUB_DEGREE_CAP) prunedIds.add(n.id);
      }
      // Nodes surviving the super-hub prune (the candidate render set).
      const hubKept = prunedIds.size
        ? allNodes.filter((n) => !prunedIds.has(n.id))
        : allNodes;
      const hubKeptEdges = prunedIds.size
        ? kindFilteredEdges.filter(
            (e) => !prunedIds.has(e.source) && !prunedIds.has(e.target),
          )
        : kindFilteredEdges;

      // #4467 — count true orphans (rendered degree 0) and degree-1 leaves so the
      // badge can report HOW MANY low-degree nodes exist, and apply the honest
      // min-degree filter on top. Degree is the RENDERED degree (respects the
      // edge-kind toggles), consistent with the canvas opacity de-emphasis.
      let orphans = 0;
      let leaves = 0;
      for (const n of hubKept) {
        const d = incident.get(n.id) ?? 0;
        if (d === 0) orphans++;
        else if (d === 1) leaves++;
      }
      const lowDegree = orphans + leaves;

      // Apply the min-degree filter (default 0 = show everything). Hiding is always
      // surfaced in the badge, so it stays explicit + reversible.
      const minD = s.minDegree;
      if (minD <= 0 && prunedIds.size === 0) {
        return {
          nodes: allNodes,
          edges: kindFilteredEdges,
          prunedHubCount: 0,
          lowDegreeCount: lowDegree,
          orphanCount: orphans,
          hiddenLowDegreeCount: 0,
        };
      }
      const keptNodes =
        minD <= 0
          ? hubKept
          : hubKept.filter((n) => (incident.get(n.id) ?? 0) >= minD);
      const keptIds = new Set(keptNodes.map((n) => n.id));
      const keptEdges =
        minD <= 0
          ? hubKeptEdges
          : hubKeptEdges.filter((e) => keptIds.has(e.source) && keptIds.has(e.target));
      return {
        nodes: keptNodes,
        edges: keptEdges,
        prunedHubCount: prunedIds.size,
        lowDegreeCount: lowDegree,
        orphanCount: orphans,
        hiddenLowDegreeCount: hubKept.length - keptNodes.length,
      };
    }, [allNodes, kindFilteredEdges, s.minDegree]);

  // ── monorepo-aware default coloring/grouping (Fix #1532-1) ───────────────────
  // A monorepo is a single repo split into many modules. Repo grouping there is
  // one flat color = useless; default to per-MODULE color + module grouping.
  // Multi-repo groups keep Repo. Applied ONCE per group, before the user touches
  // the controls (applyMonorepoDefaults no-ops once groupingTouched is true).
  useEffect(() => {
    if (!data || nodes.length === 0) return;
    const repos = new Set(nodes.map((n) => n.repo));
    const moduleOf = (sf: string) =>
      sf ? sf.replace(/\\/g, "/").split("/").slice(0, -1).slice(-2).join("/") : "";
    const modules = new Set(nodes.map((n) => moduleOf(n.sourceFile)).filter(Boolean));
    const isMonorepo = repos.size <= 1 && modules.size >= 3;
    s.applyMonorepoDefaults(isMonorepo);
    // Run once per group when data first becomes available.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupId, !!data]);

  const searchMatches = useMemo(() => {
    if (!s.search.trim()) return [];
    const q = s.search.toLowerCase();
    return nodes.filter((n) => n.label.toLowerCase().includes(q));
  }, [nodes, s.search]);

  // Click-to-focus N-hop ego-graph (1 hop).
  const adjacency = useMemo(() => {
    const m = new Map<string, Set<string>>();
    for (const e of edges) {
      if (!m.has(e.source)) m.set(e.source, new Set());
      if (!m.has(e.target)) m.set(e.target, new Set());
      m.get(e.source)!.add(e.target);
      m.get(e.target)!.add(e.source);
    }
    return m;
  }, [edges]);

  // Fix #1548-3 / #1564-4: focus builds a NEW ego sub-graph = the node +
  // neighbors up to N hops (BFS over the edge set). We render ONLY that
  // sub-graph (see `egoNodes`/`egoEdges` below) and fit the camera to it. N is
  // the store's egoHops, driven LIVE by the hops slider in the focus banner.
  const bfsEgo = (id: string, hops: number): Set<string> => {
    const set = new Set<string>([id]);
    let frontier = [id];
    for (let h = 0; h < hops; h++) {
      const next: string[] = [];
      for (const f of frontier) {
        for (const nb of adjacency.get(f) ?? []) {
          if (!set.has(nb)) {
            set.add(nb);
            next.push(nb);
          }
        }
      }
      frontier = next;
    }
    return set;
  };
  const focusEgo = (id: string) => {
    // Snapshot the current camera so EXIT can restore it exactly (#1548-3).
    canvasRef.current?.snapshotCamera();
    s.setFocusRoot(id);
    s.setFocusNodes(bfsEgo(id, s.egoHops));
  };

  // Fix #1564-4: re-run the BFS at the new depth when the hops slider moves,
  // re-rendering + re-fitting the ego sub-graph live. Only while focused.
  useEffect(() => {
    if (!s.focusRootId) return;
    s.setFocusNodes(bfsEgo(s.focusRootId, s.egoHops));
    // adjacency/focusRootId captured; re-run on hops or adjacency change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s.egoHops, s.focusRootId, adjacency]);

  // #1386 — when the user expands a module from the overview, build a focus
  // set covering all entities in that (repo, module) pair and pop back into
  // the entity-level view. We piggy-back on the existing focus machinery so
  // the rendered ego sub-graph, exit affordance, camera snapshot etc. all
  // just work — no parallel "module focus" branch.
  useEffect(() => {
    if (!s.expandedModule || !data) return;
    const { repo: targetRepo, moduleName } = s.expandedModule;
    const ids = new Set<string>();
    for (const n of nodes) {
      if (n.repo !== targetRepo) continue;
      if (moduleKeyOf(n.sourceFile) !== moduleName) continue;
      ids.add(n.id);
    }
    if (ids.size === 0) {
      // Fallback: the module identity from the daemon (Module entity Name)
      // doesn't always line up with the entity-graph's path-derived module
      // key — synthetic top-level modules carry the repo slug as their name,
      // for example. Fall back to filtering by repo so the user STILL gets a
      // meaningful expanded sub-graph (the repo's entities) rather than a
      // silent no-op.
      for (const n of nodes) {
        if (n.repo === targetRepo) ids.add(n.id);
      }
    }
    if (ids.size === 0) {
      s.setExpandedModule(null);
      return;
    }
    canvasRef.current?.snapshotCamera();
    s.setModuleOverviewMode(false); // exit overview into entity view…
    s.setExpandedModule(null);
    s.setFocusRoot(null); // …with a module-scoped focus instead of a single root.
    s.setFocusNodes(ids);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s.expandedModule, data]);

  // Once data arrives, if a node was deep-linked, focus its ego-graph.
  useEffect(() => {
    if (data && s.selectedNodeId && !s.focusNodeIds) {
      focusEgo(s.selectedNodeId);
    }
    // Only trigger when data first becomes available.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!data]);

  const selectedNode: GraphNode | null = useMemo(
    () => nodes.find((n) => n.id === s.selectedNodeId) ?? null,
    [nodes, s.selectedNodeId],
  );

  // Fix #1548-3: when an ego focus is active, render ONLY the sub-graph (node +
  // ≤5-hop neighbors). The canvas re-layouts + fits this smaller set so it
  // fills the viewport and far neighbors stay reachable. On exit we pass the
  // full sets back and restore the snapshotted camera.
  const focusActive = !!(s.focusNodeIds && s.focusNodeIds.size > 0);
  const egoNodes = useMemo(() => {
    if (!focusActive) return nodes;
    return nodes.filter((n) => s.focusNodeIds!.has(n.id));
  }, [nodes, focusActive, s.focusNodeIds]);
  const egoEdges = useMemo(() => {
    if (!focusActive) return edges;
    return edges.filter((e) => s.focusNodeIds!.has(e.source) && s.focusNodeIds!.has(e.target));
  }, [edges, focusActive, s.focusNodeIds]);
  const focusLabel = useMemo(() => {
    if (!focusActive) return "";
    const rootId = s.focusRootId ?? s.selectedNodeId;
    const root = nodes.find((n) => n.id === rootId);
    if (root) return root.label;
    return nodes.find((n) => s.focusNodeIds!.has(n.id))?.label ?? "node";
  }, [focusActive, nodes, s.focusNodeIds, s.focusRootId, s.selectedNodeId]);

  const exitFocus = () => {
    s.setFocusNodes(null);
    s.setFocusRoot(null);
    s.setSelectedNode(null);
    // Restore the full-graph camera (zoom + pan) snapshotted on focus enter.
    canvasRef.current?.restoreCamera();
  };

  const onNodeClick = (node: GraphNode | null) => {
    s.setSelectedNode(node?.id ?? null);
    if (!node) exitFocus();
  };

  // #4319 — note any pruned super-hubs in the LOD badge so the omission is
  // visible (e.g. "… · −1 hub") rather than a silent drop.
  const lodLabel =
    `${s.lod.toUpperCase()}  ${nodes.length.toLocaleString()}/${(
      data?.totalNodeCount ?? nodes.length
    ).toLocaleString()}` +
    (prunedHubCount > 0
      ? ` · −${prunedHubCount} hub${prunedHubCount === 1 ? "" : "s"}`
      : "");

  // Edge-kind filters count as "active" when they deviate from the default-on
  // set (structural kinds ON, semantic kinds OFF): each structural kind turned
  // OFF and each semantic kind turned ON is one active filter.
  const edgeKindDeviations = ALL_EDGE_KINDS.reduce((acc, k) => {
    const isDefaultOn = STRUCTURAL_EDGE_KINDS.includes(k);
    return acc + (s.enabledEdgeKinds.has(k) === isDefaultOn ? 0 : 1);
  }, 0);
  const activeFilterCount =
    (s.activeRepos?.size ?? 0) + edgeKindDeviations + (s.minDegree > 0 ? 1 : 0);

  return (
    <div className="relative flex h-full flex-col">
      {/* Toolbar */}
      <div className="flex items-center gap-2 border-b border-border bg-bg px-4 py-2">
        <div className="relative w-72">
          <SearchInput
            ref={searchRef}
            value={s.search}
            onChange={(e) => s.setSearch(e.target.value)}
            placeholder="Search entities…"
            shortcut={s.search ? undefined : "/"}
          />
          {s.search ? (
            <button
              onClick={() => s.setSearch("")}
              aria-label="Clear search"
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-text-3 hover:text-text"
            >
              <X size={14} />
            </button>
          ) : null}
        </div>
        {s.search ? (
          <span className="text-sm text-text-3 tabular-nums">
            {searchMatches.length} result{searchMatches.length === 1 ? "" : "s"}
          </span>
        ) : null}

        <div className="ml-auto flex items-center gap-2">
          {/* #1386 — Module overview toggle. Off by default; ON collapses the
              graph to module-level nodes (one node per module) so users can
              see the SCC/PageRank/betweenness from #1384 at a glance. */}
          <button
            onClick={() => s.setModuleOverviewMode(!s.moduleOverviewMode)}
            aria-pressed={s.moduleOverviewMode}
            data-testid="module-overview-toggle"
            className={`inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-sm transition-colors ${
              s.moduleOverviewMode
                ? "border-accent bg-accent-soft text-accent-strong"
                : "border-border bg-surface text-text-2 hover:bg-surface-2"
            }`}
            title="Collapse graph to module-level overview"
          >
            <Boxes size={13} /> Module overview
          </button>

          <div className="flex overflow-hidden rounded-md border border-border">
            {COLOR_MODES.map((m) => (
              <button
                key={m.id}
                onClick={() => s.setColorMode(m.id)}
                aria-pressed={s.colorMode === m.id}
                className={`h-7 px-2.5 text-xs font-medium transition-colors ${
                  s.colorMode === m.id
                    ? "bg-accent-soft text-accent-strong"
                    : "bg-surface text-text-2 hover:bg-surface-2"
                }`}
              >
                {m.label}
              </button>
            ))}
          </div>

          <CommunitiesPopover communities={data?.communities ?? []} />

          <Pill
            active={activeFilterCount > 0}
            onClick={() => s.setFiltersOpen(true)}
            count={activeFilterCount}
          >
            <SlidersHorizontal size={13} /> Filters
          </Pill>

          <button
            onClick={() => {
              s.resetView();
              s.requestRelayout();
            }}
            aria-label="Reset view"
            className="inline-flex h-7 items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 text-sm text-text-2 hover:bg-surface-2"
          >
            <RotateCcw size={13} /> Reset
          </button>
        </div>
      </div>

      {/* Intro / legend header — the landing screen otherwise has no lead-in.
          Kept compact so the canvas stays the hero. */}
      <div className="shrink-0 border-b border-border bg-bg px-4 py-2 space-y-2">
        <ScreenDescription>
          The dependency graph — every indexed entity (files, classes, functions)
          and the edges between them (imports, calls, references). Search to jump
          to a symbol, color by repo/kind/community, or collapse to a module
          overview. Node color encodes the active mode; faint peripheral nodes are
          low-degree leaves, not orphans.
        </ScreenDescription>
        <AgentUsage
          tool="archigraph_find"
          example="An agent locates a symbol and its neighbors instead of grepping."
        />
      </div>

      {/* Canvas + overlays */}
      <div className="relative min-h-0 flex-1">
        {s.moduleOverviewMode ? (
          /* #1386 — module-level collapsed view. Renders OVER the entity
             graph route's data plumbing; closing it (toggle off OR Reset)
             returns to the canonical entity-level canvas below. */
          moduleAnalysis.isLoading ? (
            <div className="grid h-full place-items-center bg-bg">
              <div className="flex flex-col items-center gap-3 text-text-3">
                <div className="h-8 w-8 animate-spin rounded-full border-2 border-border border-t-accent" />
                <span className="text-sm">Computing module analysis…</span>
              </div>
            </div>
          ) : moduleAnalysis.isError ? (
            <div className="grid h-full place-items-center bg-bg">
              <div className="text-center">
                <p className="text-md text-text">Module analysis unavailable.</p>
                <p className="mt-1 text-sm text-text-3">
                  The daemon endpoint /api/v2/groups/{groupId}/modules/analysis
                  returned an error.
                </p>
              </div>
            </div>
          ) : moduleAnalysis.data ? (
            <ModuleOverview
              data={moduleAnalysis.data}
              activeRepo={
                s.activeRepos && s.activeRepos.size === 1
                  ? Array.from(s.activeRepos)[0]
                  : null
              }
              isDark={isDark}
              onExpandModule={(m) => s.setExpandedModule(m)}
            />
          ) : null
        ) : isLoading ? (
          <div className="grid h-full place-items-center bg-bg">
            <div className="flex flex-col items-center gap-3 text-text-3">
              <div className="h-8 w-8 animate-spin rounded-full border-2 border-border border-t-accent" />
              <span className="text-sm">Loading graph…</span>
            </div>
          </div>
        ) : isError ? (
          <div className="grid h-full place-items-center bg-bg">
            <div className="text-center">
              <p className="text-md text-text">Could not load the graph.</p>
              <p className="mt-1 text-sm text-text-3">Is this group indexed? Check the daemon.</p>
            </div>
          </div>
        ) : nodes.length === 0 ? (
          <div className="grid h-full place-items-center bg-bg">
            <p className="text-md text-text-3">No nodes match the active filters.</p>
          </div>
        ) : (
          <>
            <Suspense
              fallback={
                <div className="grid h-full place-items-center bg-bg">
                  <div className="h-8 w-8 animate-spin rounded-full border-2 border-border border-t-accent" />
                </div>
              }
            >
              <GraphCanvas
                ref={setCanvasRef}
                group={focusActive ? `${groupId}::ego` : groupId}
                nodes={egoNodes}
                edges={egoEdges}
                isFocusView={focusActive}
                selectedNodeId={s.selectedNodeId}
                hoveredNodeId={s.hoveredNodeId}
                isDark={isDark}
                colorMode={s.colorMode}
                groupBy={s.groupBy}
                simulation={s.simulation}
                nodeSizing={s.nodeSizing}
                render={s.render}
                activeRepos={s.activeRepos}
                focusedCommunityId={s.focusedCommunityId}
                relayoutNonce={s.relayoutNonce}
                onNodeClick={onNodeClick}
                onNodeHover={(n) => s.setHoveredNode(n?.id ?? null)}
                onSettled={() => {}}
                highlightedNodeIds={jarvis.enabled ? jarvis.highlightedNodeIds : undefined}
                highlightEpoch={jarvis.epoch}
              />
            </Suspense>

            {/* #1932 JARVIS SVG overlay — chevrons + comet + trail tint.
                Placed BEFORE the activity overlay so the badge/log paint on
                top, but above the WebGL vignette (via z-index inside the
                overlay's own root). */}
            <GraphJarvisOverlay
              canvasHandle={liveCanvasHandle}
              steps={replay.steps}
              currentTarget={replay.snapshot.currentTarget}
              edgeProgress={replay.snapshot.edgeProgress}
              traversedEdges={new Set(replay.snapshot.traversedEdges)}
              running={replay.snapshot.running}
              paused={replay.snapshot.paused}
              phase={replay.snapshot.phase}
              glowProgress={replay.snapshot.glowProgress}
              reducedMotion={replay.reducedMotion}
              highlightedNodeIds={jarvis.enabled ? jarvis.highlightedNodeIds : undefined}
              className="z-20"
            />

            {/* #1157 Jarvis: MCP activity badge + log + on/off toggle.
                #1932: now also hosts Replay-all, speed, pause/resume, scrubber,
                audio toggle. Per-entry 🔄 + Glow toggle + dismiss X all stay. */}
            <MCPActivityOverlay
              enabled={jarvis.enabled}
              connected={jarvis.sseConnected}
              isActive={jarvis.isActive}
              totalCount={jarvis.totalCount}
              eventLog={jarvis.eventLog}
              onToggle={jarvis.setEnabled}
              onReplay={jarvis.replay}
              onClear={jarvis.clearActivityLog}
              replayController={replay.controller}
              replaySnapshot={replay.snapshot}
              replaySteps={replay.steps}
              speedKey={replay.speedKey}
              onSpeedKey={replay.setSpeedKey}
              audioOn={replay.audioOn}
              onAudioToggle={replay.setAudioOn}
            />

            {/* Fix #1548-3: clear "focused on X — exit" affordance. */}
            {focusActive ? (
              <div className="absolute left-1/2 top-3 z-30 flex -translate-x-1/2 items-center gap-2 rounded-full border border-accent/40 bg-surface/90 px-3 py-1 text-sm text-text shadow-sm backdrop-blur-sm">
                <span className="text-text-3">Focused on</span>
                <span className="max-w-[14rem] truncate font-medium">{focusLabel}</span>
                <span className="text-text-4 tabular-nums">· {egoNodes.length} nodes</span>
                {/* Fix #1564-4: hops slider — re-runs the ego BFS live. */}
                <span className="ml-1 h-4 w-px bg-border" aria-hidden />
                <label className="flex items-center gap-1.5 text-xs text-text-3">
                  <span>Hops</span>
                  <input
                    type="range"
                    min={1}
                    max={6}
                    step={1}
                    value={s.egoHops}
                    onChange={(e) => s.setEgoHops(Number(e.target.value))}
                    aria-label="Ego sub-graph hops"
                    className="h-1 w-20 cursor-pointer accent-accent"
                  />
                  <span className="w-3 tabular-nums font-medium text-text-2">{s.egoHops}</span>
                </label>
                <button
                  onClick={exitFocus}
                  aria-label="Exit focus"
                  className="ml-1 inline-flex items-center gap-1 rounded-full border border-border bg-surface px-2 py-0.5 text-xs text-text-2 hover:bg-surface-2"
                >
                  <X size={12} /> Exit
                </button>
              </div>
            ) : null}

            <div className="absolute bottom-3 left-3 z-20 flex flex-col items-start gap-1.5">
              <div className="pointer-events-none rounded-md border border-border bg-surface/80 px-2 py-1 font-mono text-xs text-text-3 backdrop-blur-sm">
                LOD: {lodLabel}
              </div>
              {/* #4467 — honest low-degree badge. The force layout pushes degree-1
                  nodes to the periphery, which reads as a misleading "orphan ring";
                  in reality almost all are degree-1 leaves (a single CONTAINS edge),
                  not true orphans. This badge names the real counts and offers an
                  explicit, reversible show/hide so nothing is ever silently dropped. */}
              {lowDegreeCount > 0 ? (
                <div className="flex items-center gap-1.5 rounded-md border border-border bg-surface/80 px-2 py-1 text-xs text-text-3 backdrop-blur-sm">
                  <span className="tabular-nums">
                    {(lowDegreeCount - orphanCount).toLocaleString()} low-degree
                  </span>
                  <span className="text-text-4">·</span>
                  <span className="tabular-nums">{orphanCount.toLocaleString()} unconnected</span>
                  {hiddenLowDegreeCount > 0 ? (
                    <span className="tabular-nums text-warning">
                      · {hiddenLowDegreeCount.toLocaleString()} hidden
                    </span>
                  ) : null}
                  <button
                    onClick={() => s.setMinDegree(s.minDegree > 0 ? 0 : 1)}
                    className="ml-0.5 rounded border border-border bg-surface px-1.5 py-0.5 text-[0.7rem] font-medium text-text-2 hover:bg-surface-2"
                    title="Toggle hiding of low-degree / unconnected nodes"
                  >
                    {s.minDegree > 0 ? "show" : "hide"}
                  </button>
                </div>
              ) : null}
            </div>

            <div className="pointer-events-none absolute bottom-3 right-3 z-20 flex items-center gap-2 text-xs text-text-4">
              drag · scroll · <Kbd>/</Kbd>
            </div>

            {selectedNode ? (
              <NodeInspector
                groupId={groupId}
                node={selectedNode}
                onClose={() => {
                  if (s.focusNodeIds) exitFocus();
                  else s.setSelectedNode(null);
                }}
                onFocusNode={(id) => focusEgo(id)}
              />
            ) : null}
          </>
        )}

        <FiltersDrawer repos={data?.repos ?? []} />
      </div>
    </div>
  );
}
