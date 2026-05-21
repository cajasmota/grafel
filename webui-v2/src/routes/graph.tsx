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

import { useEffect, useMemo, useRef } from "react";
import { useParams } from "react-router-dom";
import { X, RotateCcw, SlidersHorizontal } from "lucide-react";
import { SearchInput, Pill, Kbd } from "@/components/ui";
import { useGraph } from "@/hooks/use-graph";
import { useGraphStore, type ColorMode } from "@/store/use-graph-store";
import { useAppStore } from "@/store/use-app-store";
import type { EdgeKind, GraphNode } from "@/data/types";
import { GraphCanvas } from "@/components/graph/graph-canvas";
import { NodeInspector } from "@/components/graph/node-inspector";
import { FiltersDrawer } from "@/components/graph/filters-drawer";
import { CommunitiesPopover } from "@/components/graph/communities-popover";

const COLOR_MODES: { id: ColorMode; label: string }[] = [
  { id: "repo", label: "Repo" },
  { id: "community", label: "Community" },
  { id: "degree", label: "Degree" },
];

export default function GraphScreen() {
  const { groupId = "demo" } = useParams();
  const theme = useAppStore((st) => st.theme);
  const isDark = theme === "dark";

  const s = useGraphStore();
  const { data, isLoading, isError } = useGraph(groupId);

  const searchRef = useRef<HTMLInputElement>(null);

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
        if (s.selectedNodeId) s.setSelectedNode(null);
        else if (s.filtersOpen) s.setFiltersOpen(false);
        else if (s.communitiesOpen) s.setCommunitiesOpen(false);
        else if (s.focusNodeIds) s.setFocusNodes(null);
        else if (s.focusedCommunityId != null) s.setFocusedCommunity(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [s]);

  // Edge-kind filter applied client-side (kinds are cheap to filter locally).
  const edges = useMemo(() => {
    if (!data) return [];
    if (s.enabledEdgeKinds.size === 7) return data.edges;
    return data.edges.filter((e) => s.enabledEdgeKinds.has(e.kind as EdgeKind));
  }, [data, s.enabledEdgeKinds]);

  const nodes = data?.nodes ?? [];

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

  const focusEgo = (id: string, hops = 1) => {
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
    s.setFocusNodes(set);
  };

  const selectedNode: GraphNode | null = useMemo(
    () => nodes.find((n) => n.id === s.selectedNodeId) ?? null,
    [nodes, s.selectedNodeId],
  );

  const onNodeClick = (node: GraphNode | null) => {
    s.setSelectedNode(node?.id ?? null);
    if (!node) s.setFocusNodes(null);
  };

  const lodLabel = `${s.lod.toUpperCase()}  ${nodes.length.toLocaleString()}/${(
    data?.totalNodeCount ?? nodes.length
  ).toLocaleString()}`;

  const activeFilterCount = (s.activeRepos?.size ?? 0) + (7 - s.enabledEdgeKinds.size);

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

      {/* Canvas + overlays */}
      <div className="relative min-h-0 flex-1">
        {isLoading ? (
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
            <GraphCanvas
              group={groupId}
              nodes={nodes}
              edges={edges}
              selectedNodeId={s.selectedNodeId}
              isDark={isDark}
              colorMode={s.colorMode}
              groupBy={s.groupBy}
              simulation={s.simulation}
              nodeSizing={s.nodeSizing}
              render={s.render}
              activeRepos={s.activeRepos}
              focusedCommunityId={s.focusedCommunityId}
              focusNodeIds={s.focusNodeIds}
              relayoutNonce={s.relayoutNonce}
              onNodeClick={onNodeClick}
              onNodeHover={(n) => s.setHoveredNode(n?.id ?? null)}
              onSettled={() => {}}
            />

            <div className="pointer-events-none absolute bottom-3 left-3 z-20 rounded-md border border-border bg-surface/80 px-2 py-1 font-mono text-xs text-text-3 backdrop-blur-sm">
              LOD: {lodLabel}
            </div>

            <div className="pointer-events-none absolute bottom-3 right-3 z-20 flex items-center gap-2 text-xs text-text-4">
              drag · scroll · <Kbd>/</Kbd>
            </div>

            {selectedNode ? (
              <NodeInspector
                groupId={groupId}
                node={selectedNode}
                onClose={() => {
                  s.setSelectedNode(null);
                  s.setFocusNodes(null);
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
