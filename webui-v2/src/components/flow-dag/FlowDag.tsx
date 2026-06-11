/* ============================================================
   components/flow-dag/FlowDag.tsx — shared downstream-DAG renderer.

   A reusable React Flow view of an HTTP endpoint's DOWNSTREAM as a branching
   DAG (endpoint → handler → service → repository → pipeline, plus
   JOINS_COLLECTION / THROWS / VALIDATES side-branches). Backed by
   GET /api/v2/groups/:id/paths/:hash/downstream-dag (#4349).

   Controls:
     - H/V toggle        → tidy-tree main axis LR (horizontal) / TB (vertical).
     - depth stepper     → refetches with &depth= (clamped server-side to 1..24).
     - spine / full      → refetches with mode=spine (default; collapses
                           builder/predicate noise) / mode=full (every node).
     - expand-noise      → collapsed builder/predicate children expand inline
                           on a node, no refetch (rows ship on the payload).

   Decoupling (#4354): the component accepts EITHER a (groupId, pathHash, verb)
   triple — fetching the DAG itself via useDownstreamDAG — OR a pre-fetched
   `payload`. The future Flows-view rebuild can drive it with its own payload
   without going through the paths hook.
   ============================================================ */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  ReactFlowProvider,
  type Node as RFNode,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  ArrowRight,
  ArrowDown,
  Minus,
  Plus,
  Loader2,
  AlertTriangle,
  Play,
  Pause,
  Square,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  createFlowAnim,
  useFlowAnim,
  type FlowAnimController,
} from "@/lib/flow-animation";
import {
  playStepBlip,
  readFlowAudio,
  writeFlowAudio,
} from "@/lib/flow-audio";
import { useDownstreamDAG } from "@/hooks/use-paths";
import type { DownstreamDAGNode, DownstreamDAGResponse } from "@/data/types";
import {
  unfoldTree,
  layoutTree,
  MAX_TREE_NODES,
  FLOW_DAG_NODE_TYPE,
  FLOW_DAG_EDGE_TYPE,
  type FlowDagDirection,
  type FlowDagNodeData,
  type FlowDagEdgeData,
} from "./layout";
import { routeInstanceIds } from "./route";
import { FlowDagNode } from "./FlowDagNode";
import { FlowDagEdge } from "./FlowDagEdge";
import { FlowDagLegend } from "./FlowDagLegend";

const nodeTypes: NodeTypes = { [FLOW_DAG_NODE_TYPE]: FlowDagNode };
const edgeTypes: EdgeTypes = { [FLOW_DAG_EDGE_TYPE]: FlowDagEdge };

const DEPTH_MIN = 1;
const DEPTH_MAX = 24;
const DEPTH_DEFAULT = 8;

export interface FlowDagProps {
  groupId: string;
  /** Path hash to root the DAG at. Ignored when `payload` is supplied. */
  pathHash?: string | null;
  /** Disambiguate when a path maps to several verb endpoints. */
  verb?: string;
  /** Pre-fetched payload — bypasses the internal fetch (Flows reuse, #4354). */
  payload?: DownstreamDAGResponse;
  /** Whether the internal fetch is enabled (e.g. only when a modal is open). */
  enabled?: boolean;
  /**
   * Notified when a node is clicked, with its underlying DAG node. Lets a
   * caller open a side inspector (Flows view, #4354) without forking the
   * renderer. Omit for a purely read-only canvas (Paths modal).
   */
  onNodeClick?: (node: DownstreamDAGNode) => void;
  /** Node id to highlight as selected (pairs with `onNodeClick`). */
  selectedNodeId?: string | null;
  /**
   * Enable the step-replay overlay (#4362): a play/pause/stop + speed + scrubber
   * control bar that walks the DAG in topological order, glowing the active
   * node/edge and riding a comet along each edge. Idle on mount; resets when the
   * rendered DAG changes. Off by default (Paths modal stays a static canvas).
   */
  enableReplay?: boolean;
  className?: string;
}

// ── Step-replay hook + control bar (#4362) ───────────────────────────────────

const SPEEDS = [0.5, 1, 2] as const;
// Long flows auto-bump to 2× so they stay smooth (#1922 constraint).
const AUTO_FAST_STEPS = 80;

interface FlowReplay {
  /** Whether the replay overlay is engaged (played/scrubbed and not stopped). */
  active: boolean;
  snapshot: ReturnType<FlowAnimController["getSnapshot"]>;
  speed: number;
  audio: boolean;
  controls: {
    toggle: () => void;
    stop: () => void;
    stepForward: () => void;
    stepBack: () => void;
    scrubTo: (n: number) => void;
    setSpeed: (s: number) => void;
    toggleAudio: () => void;
  };
  totalSteps: number;
}

/**
 * Owns a createFlowAnim controller for the current DAG. Idle on mount; the
 * controller is rebuilt (and torn down) whenever the sequence length, root, or
 * orientation changes, so switching flows resets cleanly. `active` flips true
 * once the user plays/scrubs and false again on stop — so an idle canvas is
 * never dimmed.
 */
function useFlowReplay(
  edgeCount: number,
  resetKey: string | null,
  direction: FlowDagDirection,
): FlowReplay {
  const [engaged, setEngaged] = useState(false);
  const [speed, setSpeedState] = useState(() =>
    edgeCount + 1 > AUTO_FAST_STEPS ? 2 : 1,
  );
  const [audio, setAudio] = useState(() => readFlowAudio());
  const audioRef = useRef(audio);
  audioRef.current = audio;

  // Build a controller per (edgeCount, resetKey, direction). totalSteps is the
  // node count (edgeCount + 1) so the playhead spans entry..last node.
  const controller = useMemo(() => {
    const c = createFlowAnim({
      totalSteps: edgeCount + 1,
      // We don't distinguish bridge edges in the flow payload here; uniform comet.
      isBridgeEdge: () => false,
      reducedMotion:
        typeof window !== "undefined" &&
        window.matchMedia?.("(prefers-reduced-motion: reduce)").matches,
    });
    return c;
    // resetKey/direction force a fresh controller so a flow/layout change resets.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [edgeCount, resetKey, direction]);

  // Reset engagement + re-apply speed/audio wiring when the controller changes.
  useEffect(() => {
    setEngaged(false);
    controller.reset();
    controller.setSpeed(speed);
    controller.setOnArrive(() => {
      if (audioRef.current) playStepBlip();
    });
    return () => {
      controller.stop();
      controller.setOnArrive(null);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [controller]);

  const snapshot = useFlowAnim(controller);

  // ESC pauses a running replay (#1922).
  useEffect(() => {
    if (!engaged) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && snapshot.running && !snapshot.paused) {
        controller.pause();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [engaged, controller, snapshot.running, snapshot.paused]);

  const controls = useMemo(
    () => ({
      toggle: () => {
        setEngaged(true);
        controller.toggle();
      },
      stop: () => {
        controller.reset();
        setEngaged(false);
      },
      stepForward: () => {
        setEngaged(true);
        controller.scrubTo(Math.min(edgeCount + 1, controller.getSnapshot().playhead + 1));
      },
      stepBack: () => {
        setEngaged(true);
        controller.scrubTo(Math.max(0, controller.getSnapshot().playhead - 1));
      },
      scrubTo: (n: number) => {
        setEngaged(true);
        controller.scrubTo(n);
      },
      setSpeed: (s: number) => {
        setSpeedState(s);
        controller.setSpeed(s);
      },
      toggleAudio: () => {
        setAudio((prev) => {
          const next = !prev;
          writeFlowAudio(next);
          return next;
        });
      },
    }),
    [controller, edgeCount],
  );

  return {
    active: engaged,
    snapshot,
    speed,
    audio,
    controls,
    totalSteps: edgeCount + 1,
  };
}

/** The bottom replay control bar — play/pause/stop, step, speed, scrubber. */
function ReplayBar({ replay }: { replay: FlowReplay }) {
  const { snapshot, speed, audio, controls, totalSteps } = replay;
  const lastStep = Math.max(0, totalSteps - 1);
  const playing = snapshot.running && !snapshot.paused;
  // playhead counts nodes reached (1-based once started). Map to a 0..lastStep
  // scrubber position (0 = entry, lastStep = final node).
  const pos = Math.max(0, Math.min(lastStep, snapshot.playhead - 1 < 0 ? 0 : snapshot.playhead));

  if (totalSteps < 2) return null;

  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 border-t border-border bg-surface">
      <button
        type="button"
        onClick={controls.toggle}
        className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-surface text-text-2 hover:bg-surface-2 transition-colors"
        title={playing ? "Pause replay" : "Play replay"}
      >
        {playing ? <Pause size={13} /> : <Play size={13} />}
      </button>
      <button
        type="button"
        onClick={controls.stop}
        disabled={!replay.active}
        className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-surface text-text-2 hover:bg-surface-2 transition-colors disabled:opacity-40 disabled:pointer-events-none"
        title="Stop replay"
      >
        <Square size={12} />
      </button>
      <button
        type="button"
        onClick={controls.stepBack}
        className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-surface text-text-2 hover:bg-surface-2 transition-colors"
        title="Step back"
      >
        <SkipBack size={13} />
      </button>
      <button
        type="button"
        onClick={controls.stepForward}
        className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-surface text-text-2 hover:bg-surface-2 transition-colors"
        title="Step forward"
      >
        <SkipForward size={13} />
      </button>

      {/* Scrubber — drag to jump (instant, no intermediate animation). */}
      <input
        type="range"
        min={0}
        max={lastStep}
        step={1}
        value={pos}
        onChange={(e) => controls.scrubTo(Number(e.target.value))}
        className="flex-1 min-w-[120px] accent-[var(--accent)] cursor-pointer"
        aria-label="Replay timeline"
        title={`Step ${pos} of ${lastStep}`}
      />
      <span className="text-[11px] tabular-nums text-text-3 w-14 text-center shrink-0">
        {pos} / {lastStep}
      </span>

      {/* Speed segmented control */}
      <div className="inline-flex rounded-md border border-border overflow-hidden shrink-0">
        {SPEEDS.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => controls.setSpeed(s)}
            className={cn(
              "h-7 px-1.5 text-[11px] tabular-nums transition-colors",
              s !== SPEEDS[0] && "border-l border-border",
              speed === s
                ? "bg-accent text-accent-text"
                : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title={`${s}× speed`}
          >
            {s}×
          </button>
        ))}
      </div>

      {/* Audio toggle — off by default, persisted (#1922). */}
      <button
        type="button"
        onClick={controls.toggleAudio}
        className={cn(
          "inline-flex items-center justify-center size-7 rounded-md border border-border transition-colors shrink-0",
          audio ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
        )}
        title={audio ? "Mute step blips" : "Enable step blips"}
      >
        {audio ? <Volume2 size={13} /> : <VolumeX size={13} />}
      </button>
    </div>
  );
}

/** Inner view — assumes a ReactFlowProvider is mounted above it. */
function FlowDagInner({
  groupId,
  pathHash,
  verb,
  payload,
  enabled = true,
  onNodeClick,
  selectedNodeId,
  enableReplay = false,
  className,
}: FlowDagProps) {
  // Controls — fetch params. Changing mode/depth refetches (TanStack caches
  // each combination); orientation + inline-expand are pure client state.
  const [mode, setMode] = useState<"spine" | "full">("spine");
  const [depth, setDepth] = useState<number>(payload?.depth ?? DEPTH_DEFAULT);
  const [direction, setDirection] = useState<FlowDagDirection>("LR");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  // Click-to-highlight (#4479): the focused instance id whose route is lit, or
  // null for the normal (everything-visible) view.
  const [routeFocus, setRouteFocus] = useState<string | null>(null);

  // Only fetch when no payload was injected.
  const query = useDownstreamDAG(
    groupId,
    payload ? null : pathHash ?? null,
    { mode, depth, semantic: true, verb },
    enabled && !payload,
  );

  const data: DownstreamDAGResponse | undefined = payload ?? query.data;
  const isLoading = !payload && query.isLoading;
  const error = !payload ? query.error : null;

  const onToggleExpand = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  // Unfold the deduped DAG into a pure tree (#4479). Memoized off the payload +
  // depth; instances are path-keyed so a node reached via N paths duplicates.
  const unfold = useMemo(() => {
    if (!data) return null;
    return unfoldTree(data.root_id, data.nodes, data.edges, MAX_TREE_NODES);
  }, [data]);

  // The route highlight set: ancestors + the focus + its whole forward subtree.
  const routeSet = useMemo(() => {
    if (!unfold || routeFocus == null) return null;
    return routeInstanceIds(unfold.instances, routeFocus);
  }, [unfold, routeFocus]);

  const { nodes: baseNodes, edges: baseEdges } = useMemo(() => {
    if (!unfold) return { nodes: [], edges: [] };
    const laid = layoutTree(
      unfold.instances,
      direction,
      expanded,
      onToggleExpand,
      unfold.hasOutEdge,
    );
    for (const n of laid.nodes) {
      // Selection is driven by the ORIGINAL node id so the caller's contract
      // (Flows step inspector, #4354) is unchanged across the tree unfold; all
      // instances of a selected node light up.
      if (selectedNodeId != null) {
        n.data.selected = (n.data as FlowDagNodeData).node.id === selectedNodeId;
      }
      if (routeSet) n.data.onRoute = routeSet.has(n.id);
    }
    if (routeSet) {
      for (const e of laid.edges) {
        // An edge is on the route iff BOTH its endpoints are (the tree's single
        // in-edge per node makes this exact).
        e.data = { ...e.data, kind: e.data?.kind ?? "CALLS", onRoute: routeSet.has(e.source) && routeSet.has(e.target) };
      }
    }
    return laid;
  }, [unfold, direction, expanded, onToggleExpand, selectedNodeId, routeSet]);

  // ── Step-replay (#4362) ──────────────────────────────────────────────────
  // The replay walks the laid-out tree in topological order. baseEdges are
  // emitted parent-before-child (BFS), so their natural order already respects
  // the DAG topology: edge[i] is only reachable after its source's in-edge. We
  // replay one edge per step; step i (1-based) rides baseEdges[i-1].
  //
  // The sequence + controller live behind enableReplay so the Paths modal stays
  // a static canvas. Everything is keyed on (root_id + edge count + direction)
  // so the controller resets when the flow — or its layout — changes.
  const replaySeq = useMemo(
    () =>
      enableReplay
        ? baseEdges.map((e) => ({ id: e.id, source: e.source, target: e.target }))
        : [],
    [enableReplay, baseEdges],
  );

  const replay = useFlowReplay(replaySeq.length, data?.root_id ?? null, direction);

  // Decorate nodes/edges with replay state derived from the controller snapshot.
  // playhead = number of steps completed (0 = entry only). The comet is riding
  // edge index `currentTarget - 1`… but our sequence is 1:1 with edges, so the
  // active edge is index (playhead) when running toward it. The controller's
  // `currentTarget` is the target STEP index; for our edge-indexed sequence the
  // in-flight edge is `currentTarget - 1` (0-based into replaySeq), with
  // edgeProgress 0..1 along it.
  const { nodes, edges } = useMemo(() => {
    if (!enableReplay || replaySeq.length === 0 || !replay.active) {
      return { nodes: baseNodes, edges: baseEdges };
    }
    const { playhead, currentTarget, edgeProgress, running, traversedEdges } =
      replay.snapshot;
    // Edges already traversed (target step ≤ playhead): tinted trail. The
    // controller exposes traversedEdges as target-step indices [1..playhead].
    const traversedSet = new Set(traversedEdges);
    // The in-flight edge, if the comet is mid-ride (running, between nodes).
    const activeEdgeIdx = running && currentTarget >= 1 ? currentTarget - 1 : -1;

    // Reached node instance ids: the entry plus every traversed edge's target,
    // plus the active edge's source (the comet is leaving it).
    const reached = new Set<string>();
    if (replaySeq.length > 0) reached.add(replaySeq[0].source);
    replaySeq.forEach((e, i) => {
      // replaySeq[i]'s "target step" is i+1.
      if (traversedSet.has(i + 1)) reached.add(e.target);
    });
    const activeEdge = activeEdgeIdx >= 0 ? replaySeq[activeEdgeIdx] : null;
    if (activeEdge) reached.add(activeEdge.source);
    const activeNodeId = activeEdge
      ? edgeProgress >= 1
        ? activeEdge.target
        : activeEdge.source
      : // Idle/scrubbed: the active node is the one the playhead landed on.
        playhead > 0 && playhead <= replaySeq.length
        ? replaySeq[playhead - 1].target
        : replaySeq[0]?.source ?? null;

    const nodes = baseNodes.map((n) => {
      let st: FlowDagNodeData["replay"] = "pending";
      if (n.id === activeNodeId) st = "active";
      else if (reached.has(n.id)) st = "traversed";
      return { ...n, data: { ...n.data, replay: st } as FlowDagNodeData };
    });

    const edges = baseEdges.map((e, i) => {
      let st: FlowDagEdgeData["replay"] = "pending";
      let prog: number | undefined;
      if (i === activeEdgeIdx) {
        st = "active";
        prog = edgeProgress;
      } else if (traversedSet.has(i + 1)) {
        st = "traversed";
      }
      return {
        ...e,
        data: { ...(e.data ?? { kind: "CALLS" }), replay: st, replayProgress: prog } as FlowDagEdgeData,
      };
    });

    return { nodes, edges };
  }, [enableReplay, replaySeq, replay.active, replay.snapshot, baseNodes, baseEdges]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, n: RFNode) => {
      // Toggle the route highlight: same instance again clears it (#4479).
      setRouteFocus((prev) => (prev === n.id ? null : n.id));
      // Preserve the caller's detail/selection behavior — pass the original node.
      onNodeClick?.((n.data as FlowDagNodeData).node);
    },
    [onNodeClick],
  );

  // Clicking the empty canvas clears the route highlight (#4479).
  const handlePaneClick = useCallback(() => setRouteFocus(null), []);

  // Effective truncation: OR the payload's flags with our node-cap clip so the
  // legend stays honest when the pure-tree unfold itself hits the cap.
  const effectiveTruncation = useMemo(() => {
    if (!data) return undefined;
    return {
      ...data.truncation,
      node_truncated: data.truncation.node_truncated || (unfold?.capped ?? false),
    };
  }, [data, unfold]);

  const setDepthClamped = (n: number) =>
    setDepth(Math.max(DEPTH_MIN, Math.min(DEPTH_MAX, n)));

  return (
    <div className={cn("flex flex-col h-full min-h-0", className)}>
      {/* Controls bar */}
      <div className="flex flex-wrap items-center gap-2 px-3 py-2 border-b border-border bg-surface">
        {/* H/V toggle → tidy-tree main axis */}
        <div className="inline-flex rounded-md border border-border overflow-hidden">
          <button
            type="button"
            onClick={() => setDirection("LR")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors",
              direction === "LR" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Horizontal layout (left → right)"
          >
            <ArrowRight size={12} /> H
          </button>
          <button
            type="button"
            onClick={() => setDirection("TB")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors border-l border-border",
              direction === "TB" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Vertical layout (top → bottom)"
          >
            <ArrowDown size={12} /> V
          </button>
        </div>

        {/* spine / full mode */}
        <div className="inline-flex rounded-md border border-border overflow-hidden">
          <button
            type="button"
            onClick={() => setMode("spine")}
            className={cn(
              "h-7 px-2 text-xs transition-colors",
              mode === "spine" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Spine — collapse query-builder/predicate noise into owning nodes"
          >
            Spine
          </button>
          <button
            type="button"
            onClick={() => setMode("full")}
            className={cn(
              "h-7 px-2 text-xs transition-colors border-l border-border",
              mode === "full" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Full — expand every reachable node"
          >
            Full
          </button>
        </div>

        {/* depth stepper */}
        <div className="inline-flex items-center gap-1 h-7 rounded-md border border-border px-1.5 text-xs text-text-3">
          <span className="text-text-4">depth</span>
          <button
            type="button"
            onClick={() => setDepthClamped(depth - 1)}
            disabled={depth <= DEPTH_MIN}
            className="inline-flex items-center justify-center size-5 rounded hover:bg-surface-2 disabled:opacity-40 disabled:pointer-events-none"
            title="Decrease depth"
          >
            <Minus size={11} />
          </button>
          <span className="w-4 text-center tabular-nums text-text">{depth}</span>
          <button
            type="button"
            onClick={() => setDepthClamped(depth + 1)}
            disabled={depth >= DEPTH_MAX}
            className="inline-flex items-center justify-center size-5 rounded hover:bg-surface-2 disabled:opacity-40 disabled:pointer-events-none"
            title="Increase depth"
          >
            <Plus size={11} />
          </button>
        </div>

        {/* Fetch status */}
        {isLoading && (
          <span className="inline-flex items-center gap-1 text-xs text-text-4">
            <Loader2 size={12} className="animate-spin" /> loading…
          </span>
        )}

        {/* root path/verb label */}
        {data && (
          <span className="ml-auto inline-flex items-center gap-1.5 text-xs text-text-3 font-mono truncate max-w-[40%]" title={`${data.verb} ${data.path}`}>
            <span className="font-semibold text-text-2">{data.verb}</span>
            <span className="truncate">{data.path}</span>
          </span>
        )}
      </div>

      {/* Canvas */}
      <div className="relative flex-1 min-h-0">
        {error ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-text-3">
            <AlertTriangle size={20} className="text-[var(--danger)]" />
            <p className="text-sm">Couldn't load the downstream DAG.</p>
            <p className="text-xs text-text-4">{error instanceof Error ? error.message : "Unknown error"}</p>
          </div>
        ) : data && data.nodes.length === 0 ? (
          <div className="absolute inset-0 flex items-center justify-center text-sm text-text-4">
            No downstream nodes for this endpoint.
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            onNodeClick={handleNodeClick}
            onPaneClick={handlePaneClick}
            fitView
            // The graph is a read-only visualization; disable interaction edits.
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            minZoom={0.15}
            // Re-fit when orientation flips so the whole DAG stays visible.
            fitViewOptions={{ padding: 0.18 }}
          >
            <Background gap={18} size={1} color="var(--border)" />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable className="!bg-surface !border !border-border" />
          </ReactFlow>
        )}
      </div>

      {/* Step-replay control bar (#4362) — only when the caller opts in and the
          DAG has at least one edge to walk. */}
      {enableReplay && replaySeq.length > 0 && <ReplayBar replay={replay} />}

      {/* Legend + truncation/branch stats. Node count reflects the unfolded
          tree (instances), not the deduped payload, so it matches what's drawn. */}
      {data && effectiveTruncation && (
        <FlowDagLegend
          branchCount={data.branch_count}
          nodeCount={unfold?.instances.length ?? data.nodes.length}
          truncation={effectiveTruncation}
        />
      )}
    </div>
  );
}

/**
 * FlowDag — shared downstream-DAG renderer. Wraps the inner view in a
 * ReactFlowProvider so it is drop-in anywhere (modal today, Flows view in
 * #4354) without the caller wiring provider context.
 */
export function FlowDag(props: FlowDagProps) {
  return (
    <ReactFlowProvider>
      <FlowDagInner {...props} />
    </ReactFlowProvider>
  );
}
