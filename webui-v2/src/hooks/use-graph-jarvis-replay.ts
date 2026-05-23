/* ============================================================
   hooks/use-graph-jarvis-replay.ts — Replay-all controller for the graph
   view's MCP activity panel (#1932).

   Wraps the generic `createFlowAnim` step engine (originally shipped for the
   flow DAG in #1922) with logic specific to the graph view:

     • Builds a STEP list by flattening every event's returned_node_ids in
       order. Each step lights one node and the comet rides the edge from the
       previous step's node to it. Cross-event boundaries are real edges in
       the timeline too (so the agent's hop between investigations animates).
     • Per #1932, replay-all calls the existing per-entry replay handler for
       each event (re-using the same daemon-side glow path). The graph canvas
       receives the same epoch bumps it already reacts to, AND the SVG
       overlay paints the comet/trail/chevron on top.
     • Edges that cross repos travel slower (~450ms vs ~300ms) per #1932-11.
     • Speed (0.5×/1×/2×) + audio toggle persisted to localStorage with
       graph-scoped keys (independent of the flow view's keys).
     • ESC pauses/resumes while a replay is running (matches flows' behavior).
     • Audio default OFF, persisted in `archigraph:graph:audio`.

   The hook is ALWAYS instantiated so the host can wire the scrubber UI
   without conditional hooks. When there's no event log it stays idle.
   ============================================================ */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  createFlowAnim,
  type FlowAnimController,
  type FlowAnimSnapshot,
  useFlowAnim,
} from "@/lib/flow-animation";
import { usePrefersReducedMotion } from "@/lib/use-reduced-motion";
import { playStepBlip, readGraphAudio, writeGraphAudio } from "@/lib/graph-audio";
import type { MCPActivityEvent } from "./use-mcp-activity";
import type { JarvisStep } from "@/components/graph/graph-jarvis-overlay";

// Storage keys are graph-scoped so flow + graph preferences don't clobber.
const SPEED_KEY = "archigraph:graph:speed";

export const GRAPH_SPEEDS: Array<{ key: string; mult: number; label: string }> = [
  { key: "0.5", mult: 0.5, label: "0.5×" },
  { key: "1", mult: 1, label: "1×" },
  { key: "2", mult: 2, label: "2×" },
];

// Default base speeds — matches #1932 spec (300ms regular / 450ms bridge).
const BASE_EDGE_MS = 300;
const BRIDGE_EDGE_MS = 450;
// Inter-call delay (between two MCP-event boundaries). Within a single event's
// chain we keep the engine's default 90ms inter-step.
const INTER_CALL_MS = 220;

export interface UseGraphJarvisReplayOpts {
  eventLog: MCPActivityEvent[];
  /** Per-entry replay handler (the existing glow path). */
  onReplayEvent: (event: MCPActivityEvent) => void;
  /** Map node-id pair → bridge? (resolves cross-repo edges). */
  isBridgeEdge?: (src: string, tgt: string) => boolean;
}

export interface UseGraphJarvisReplayReturn {
  /** Linear step timeline (one entry per node arrival). */
  steps: JarvisStep[];
  /** rAF snapshot from the engine. */
  snapshot: FlowAnimSnapshot;
  /** Engine controller (start / stop / pause / scrub / setSpeed). */
  controller: FlowAnimController | null;
  /** True while replay-all is running OR paused mid-flow. */
  isReplaying: boolean;
  /** Total step count (= flattened node-id length across the event log). */
  totalSteps: number;
  /** Current persisted speed key. */
  speedKey: string;
  setSpeedKey: (k: string) => void;
  /** Current audio toggle (OFF by default, per #1932-12). */
  audioOn: boolean;
  setAudioOn: (on: boolean) => void;
  /** prefers-reduced-motion: when true, comet/pulse/bounce are skipped. */
  reducedMotion: boolean;
}

export function useGraphJarvisReplay(
  opts: UseGraphJarvisReplayOpts,
): UseGraphJarvisReplayReturn {
  const { eventLog, onReplayEvent, isBridgeEdge } = opts;
  const reducedMotion = usePrefersReducedMotion();

  // ── Build the step timeline ───────────────────────────────────────────────
  const steps = useMemo<JarvisStep[]>(() => {
    const out: JarvisStep[] = [];
    eventLog.forEach((ev, evIdx) => {
      const ids = ev.returned_node_ids ?? [];
      // Cap each event's contribution so a pathological response (thousands of
      // ids) can't blow the timeline; the comet still walks the first 50 hits.
      const capped = ids.slice(0, 50);
      for (const id of capped) {
        out.push({ nodeId: id, eventIdx: evIdx, label: ev.tool_name });
      }
    });
    return out;
  }, [eventLog]);

  // ── Audio + speed prefs (persisted) ──────────────────────────────────────
  const [audioOn, setAudioOnState] = useState<boolean>(() => readGraphAudio());
  const setAudioOn = useCallback((on: boolean) => {
    setAudioOnState(on);
    writeGraphAudio(on);
  }, []);
  const audioOnRef = useRef(audioOn);
  audioOnRef.current = audioOn;

  const [speedKey, setSpeedKeyState] = useState<string>(() => {
    try {
      const saved = localStorage.getItem(SPEED_KEY);
      if (saved && GRAPH_SPEEDS.some((s) => s.key === saved)) return saved;
    } catch {
      /* ignore */
    }
    return "1";
  });
  const setSpeedKey = useCallback((k: string) => {
    setSpeedKeyState(k);
    try {
      localStorage.setItem(SPEED_KEY, k);
    } catch {
      /* ignore */
    }
  }, []);
  const speedMult = GRAPH_SPEEDS.find((s) => s.key === speedKey)?.mult ?? 1;

  // ── Step-index → eventIdx map (used to fire the per-event glow on arrival
  //    AT the FIRST node of each event). We don't fire the existing glow path
  //    for every node — only at event boundaries — so we don't spam the
  //    daemon on a multi-node response. The MCP glow already lights the full
  //    set when the event fires.
  const firstStepOfEventRef = useRef<Map<number, number>>(new Map());
  useEffect(() => {
    const m = new Map<number, number>();
    steps.forEach((s, i) => {
      if (!m.has(s.eventIdx)) m.set(s.eventIdx, i);
    });
    firstStepOfEventRef.current = m;
  }, [steps]);

  // ── isBridge resolver passed to the engine ───────────────────────────────
  // edgeIdx in the engine == target step index (≥ 1). We resolve the two
  // node ids that the comet rides between and ask the canvas if that pair
  // crosses repos. When the canvas handle isn't wired yet we conservatively
  // return false (regular ~300ms edge).
  const isBridgeEdgeRef = useRef(isBridgeEdge);
  isBridgeEdgeRef.current = isBridgeEdge;
  const stepsRef = useRef(steps);
  stepsRef.current = steps;

  const resolveBridge = useCallback((edgeIdx: number): boolean => {
    const s = stepsRef.current;
    if (edgeIdx <= 0 || edgeIdx >= s.length) return false;
    const fn = isBridgeEdgeRef.current;
    if (!fn) return false;
    return fn(s[edgeIdx - 1].nodeId, s[edgeIdx].nodeId);
  }, []);

  // ── Controller lifecycle ─────────────────────────────────────────────────
  // We recreate the controller whenever the timeline LENGTH changes; mid-run
  // changes to the underlying event log are rare (a new MCP event would add a
  // step at the tail) and recreating loses the playhead, which is annoying.
  // We accept that for v1; future iteration can grow the controller in-place.
  const onReplayEventRef = useRef(onReplayEvent);
  onReplayEventRef.current = onReplayEvent;

  const eventLogRef = useRef<MCPActivityEvent[]>(eventLog);
  eventLogRef.current = eventLog;
  const reducedMotionRef = useRef(reducedMotion);
  reducedMotionRef.current = reducedMotion;

  const totalSteps = steps.length;

  // Controller lifecycle (StrictMode-safe).
  // -----------------------------------------------------------------
  // Earlier versions of this hook created the controller inside a
  // useEffect and stopped it in the cleanup. Under React 18 StrictMode
  // dev mode the cleanup runs between the double-invoked mount, which
  // leaves the controller in state STOPPED — the user clicks Replay-all
  // and the engine fires the first couple of arrivals before its still-
  // pending interStepTimer gets cleared by the cleanup, then freezes.
  //
  // We own the controller in a ref that we (re)build in the render phase
  // ONLY when the engine is idle (no replay in flight) AND the total step
  // count has shifted. This survives the StrictMode dev double-render
  // because the second render sees `controllerStepsRef.current === totalSteps`
  // and skips the rebuild path. There's NO useEffect cleanup that could
  // clobber a running engine.
  const controllerRef = useRef<FlowAnimController | null>(null);
  const controllerStepsRef = useRef<number>(-2);

  // Re-create the controller whenever totalSteps changes AND no replay is in
  // flight. Once a replay is running we leave the engine alone (the user can
  // restart afterwards and pick up the new tail). This avoids the StrictMode
  // cleanup-clobber trap because cleanup never runs as a useEffect cleanup —
  // the controller is owned by a render-phase ref guarded against re-entry.
  const livingSnap = controllerRef.current?.getSnapshot();
  const inFlight = !!livingSnap && (livingSnap.running || livingSnap.paused);
  const needsRebuild =
    !inFlight && controllerStepsRef.current !== totalSteps;

  if (needsRebuild) {
    controllerStepsRef.current = totalSteps;
    if (controllerRef.current) {
      controllerRef.current.stop();
      controllerRef.current.reset();
      controllerRef.current = null;
    }
    if (totalSteps >= 2) {
      const c = createFlowAnim({
        totalSteps,
        isBridgeEdge: resolveBridge,
        baseEdgeMs: BASE_EDGE_MS,
        bridgeEdgeMs: BRIDGE_EDGE_MS,
        interStepMs: INTER_CALL_MS,
        speed: speedMult,
        reducedMotion: reducedMotionRef.current,
      });
      c.setOnArrive((stepIdx) => {
        const s = stepsRef.current[stepIdx];
        if (!s) return;
        const firstIdx = firstStepOfEventRef.current.get(s.eventIdx);
        if (firstIdx === stepIdx) {
          const ev = eventLogRef.current[s.eventIdx];
          if (ev) onReplayEventRef.current(ev);
        }
        if (audioOnRef.current) playStepBlip();
      });
      controllerRef.current = c;
    }
  }
  const controller = controllerRef.current;

  // Note: we deliberately do NOT run a useEffect cleanup that calls
  // controller.stop()/reset() on unmount. React 18 StrictMode dev mode
  // would invoke that cleanup between the dev-mode double-mount, which
  // would clear the controller's rAF + interStepTimer mid-replay and
  // freeze the engine at step 2. The engine's only side-effects are
  // timers; the engine is GC'd when the host route unmounts (no global
  // listeners), so the route's natural unmount is sufficient cleanup.

  // Live-push speed updates without recreating the controller.
  useEffect(() => {
    controller?.setSpeed(speedMult);
  }, [controller, speedMult]);

  // ── ESC pauses / resumes during replay (per #1932-7). The badge panel
  //    already has its own ESC handler to close itself; we only intercept
  //    when a replay is actively running.
  useEffect(() => {
    if (!controller) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      const snap = controller.getSnapshot();
      if (snap.running && !snap.paused) {
        controller.pause();
        e.stopPropagation();
      } else if (snap.paused) {
        controller.resume();
        e.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey, { capture: true });
    return () => window.removeEventListener("keydown", onKey, { capture: true } as EventListenerOptions);
  }, [controller]);

  // ── Snapshot ─────────────────────────────────────────────────────────────
  const snapshot = useFlowAnim(controller ?? IDLE_CONTROLLER);
  const isReplaying = !!controller && (snapshot.running || snapshot.paused);

  return {
    steps,
    snapshot,
    controller,
    isReplaying,
    totalSteps,
    speedKey,
    setSpeedKey,
    audioOn,
    setAudioOn,
    reducedMotion,
  };
}

// ── Idle controller — used by useFlowAnim when no real controller exists ─
// (so the snapshot type stays stable even when total step count < 2).
const IDLE_SNAPSHOT: FlowAnimSnapshot = {
  currentTarget: -1,
  playhead: 0,
  edgeProgress: 0,
  traversedEdges: [],
  lastScrubDir: null,
  running: false,
  paused: false,
};
const IDLE_CONTROLLER: FlowAnimController = {
  subscribe: () => () => {},
  getSnapshot: () => IDLE_SNAPSHOT,
  start: () => {},
  stop: () => {},
  pause: () => {},
  resume: () => {},
  toggle: () => {},
  scrubTo: () => {},
  reset: () => {},
  setSpeed: () => {},
  setOnArrive: () => {},
};
