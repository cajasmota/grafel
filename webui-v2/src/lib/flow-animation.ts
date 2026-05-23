/* ============================================================
   flow-animation.ts — replay-all state machine for the flow DAG.

   Responsibilities (per #1922):
     • Drive a glowing comet along each edge i-1 → i in sequence.
     • Track traversed-edge set so the renderer can tint them.
     • Support pause/resume mid-edge (freezes comet progress).
     • Support scrub (instant jump, no intermediate animation, with
       reverse-decay fade applied by the renderer based on lastDir).
     • Speed multiplier scales both inter-step delay and edge traversal.
     • Slower comet on cross-repo bridge edges (~450ms vs ~300ms).

   Implementation is a rAF-driven step machine kept outside React so we
   never thrash component re-renders on every animation tick — the
   renderer subscribes via getSnapshot() + a versioned counter exposed
   through subscribe().
   ============================================================ */

import { useSyncExternalStore } from "react";

export type FlowAnimSnapshot = {
  // Index of the *target* step the comet is heading toward. -1 if idle.
  // When idle (after a full Replay-all), settles on totalSteps - 1.
  currentTarget: number;
  // Number of steps the playhead is at (0 = entry only, N = full chain).
  // Equivalent to "edges traversed" + 1.
  playhead: number;
  // 0..1 progress along the current edge. 0 when not traveling.
  edgeProgress: number;
  // Set of edge indices (i = target step index) currently considered
  // "traversed" (tinted). Exposed as a sorted array for renderer use.
  traversedEdges: number[];
  // Latest scrub direction — "forward" or "backward". Lets the renderer
  // decide whether to fade newly-removed edges (reverse decay).
  lastScrubDir: "forward" | "backward" | null;
  // True if the engine is currently animating an edge (not paused / idle).
  running: boolean;
  // True if user paused mid-flow.
  paused: boolean;
};

export type FlowAnimController = {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => FlowAnimSnapshot;
  start: () => void;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  toggle: () => void;
  scrubTo: (playhead: number) => void;
  reset: () => void;
  setSpeed: (mult: number) => void;
  // Set on each arrival; renderer wires it to playStepBlip when audio is on.
  setOnArrive: (cb: ((stepIdx: number) => void) | null) => void;
};

export type CreateOpts = {
  totalSteps: number;
  isBridgeEdge: (edgeIdx: number) => boolean; // edgeIdx = target step idx ≥ 1
  baseEdgeMs?: number;     // default 300
  bridgeEdgeMs?: number;   // default 450
  interStepMs?: number;    // delay between arrival and next departure (default 90)
  speed?: number;          // default 1
  reducedMotion?: boolean; // if true, skip rAF — instant jumps only
};

const noop = () => {};

const EMPTY: FlowAnimSnapshot = {
  currentTarget: -1,
  playhead: 0,
  edgeProgress: 0,
  traversedEdges: [],
  lastScrubDir: null,
  running: false,
  paused: false,
};

export function createFlowAnim(opts: CreateOpts): FlowAnimController {
  const {
    totalSteps,
    isBridgeEdge,
    baseEdgeMs = 300,
    bridgeEdgeMs = 450,
    interStepMs = 90,
    reducedMotion = false,
  } = opts;

  let speed = opts.speed ?? 1;
  let onArrive: ((stepIdx: number) => void) | null = null;

  let snap: FlowAnimSnapshot = { ...EMPTY };
  const listeners = new Set<() => void>();

  // rAF / timing state
  let rafId: number | null = null;
  let edgeStartTs = 0;
  let edgeDurMs = 0;
  let edgeProgressFrozen = 0; // edge progress captured at pause time
  let interStepTimer: ReturnType<typeof setTimeout> | null = null;

  function emit() {
    listeners.forEach((l) => l());
  }

  function clearTimers() {
    if (rafId != null) {
      cancelAnimationFrame(rafId);
      rafId = null;
    }
    if (interStepTimer != null) {
      clearTimeout(interStepTimer);
      interStepTimer = null;
    }
  }

  function setTraversed(upTo: number): number[] {
    // Traversed edges are all edges with target idx in [1..upTo].
    const arr: number[] = [];
    for (let i = 1; i <= upTo; i++) arr.push(i);
    return arr;
  }

  function tickEdge(ts: number) {
    if (snap.paused || !snap.running) return;
    const elapsed = ts - edgeStartTs;
    const p = Math.min(1, elapsed / Math.max(1, edgeDurMs));
    snap = { ...snap, edgeProgress: p };
    emit();
    if (p >= 1) {
      // Arrive on snap.currentTarget.
      const arrived = snap.currentTarget;
      snap = {
        ...snap,
        edgeProgress: 0,
        playhead: arrived + 1, // playhead counts nodes reached (1-based)
        traversedEdges: setTraversed(arrived),
      };
      emit();
      onArrive?.(arrived);
      // Schedule next edge or stop.
      if (arrived < totalSteps - 1) {
        interStepTimer = setTimeout(() => {
          interStepTimer = null;
          beginEdge(arrived + 1);
        }, interStepMs / Math.max(0.0001, speed));
      } else {
        // Finished.
        snap = { ...snap, running: false, currentTarget: arrived };
        rafId = null;
        emit();
      }
      return;
    }
    rafId = requestAnimationFrame(tickEdge);
  }

  function beginEdge(targetIdx: number) {
    if (targetIdx <= 0 || targetIdx >= totalSteps) {
      // Nothing to animate.
      snap = { ...snap, running: false, edgeProgress: 0 };
      emit();
      return;
    }
    const baseMs = isBridgeEdge(targetIdx) ? bridgeEdgeMs : baseEdgeMs;
    edgeDurMs = baseMs / Math.max(0.0001, speed);
    edgeStartTs = performance.now();
    edgeProgressFrozen = 0;
    snap = {
      ...snap,
      currentTarget: targetIdx,
      edgeProgress: 0,
      running: true,
      paused: false,
    };
    emit();
    if (reducedMotion) {
      // Skip the comet — jump straight to arrival.
      snap = {
        ...snap,
        edgeProgress: 0,
        playhead: targetIdx + 1,
        traversedEdges: setTraversed(targetIdx),
      };
      emit();
      onArrive?.(targetIdx);
      if (targetIdx < totalSteps - 1) {
        interStepTimer = setTimeout(() => {
          interStepTimer = null;
          beginEdge(targetIdx + 1);
        }, interStepMs / Math.max(0.0001, speed));
      } else {
        snap = { ...snap, running: false };
        emit();
      }
      return;
    }
    rafId = requestAnimationFrame(tickEdge);
  }

  function start() {
    if (totalSteps < 2) return;
    clearTimers();
    snap = {
      currentTarget: 0,
      playhead: 1, // entry is already "reached"
      edgeProgress: 0,
      traversedEdges: [],
      lastScrubDir: "forward",
      running: true,
      paused: false,
    };
    emit();
    beginEdge(1);
  }

  function stop() {
    clearTimers();
    snap = {
      ...snap,
      running: false,
      paused: false,
      edgeProgress: 0,
    };
    emit();
  }

  function pause() {
    if (!snap.running || snap.paused) return;
    edgeProgressFrozen = snap.edgeProgress;
    if (rafId != null) cancelAnimationFrame(rafId);
    rafId = null;
    if (interStepTimer != null) {
      clearTimeout(interStepTimer);
      interStepTimer = null;
    }
    snap = { ...snap, paused: true };
    emit();
  }

  function resume() {
    if (!snap.paused) return;
    // Resume the in-flight edge by shifting edgeStartTs back so that
    // (now - edgeStartTs) / edgeDurMs equals the frozen progress.
    const offset = edgeProgressFrozen * edgeDurMs;
    edgeStartTs = performance.now() - offset;
    snap = { ...snap, paused: false };
    emit();
    rafId = requestAnimationFrame(tickEdge);
  }

  function toggle() {
    if (!snap.running && snap.paused === false && snap.playhead < totalSteps) {
      // Idle → start (or resume from where we are)
      if (snap.playhead > 0 && snap.playhead < totalSteps) {
        // Continue from current playhead.
        snap = { ...snap, running: true, paused: false };
        emit();
        beginEdge(snap.playhead);
        return;
      }
      start();
      return;
    }
    if (snap.running && !snap.paused) {
      pause();
    } else if (snap.paused) {
      resume();
    }
  }

  function scrubTo(target: number) {
    const clamped = Math.max(0, Math.min(totalSteps, target));
    const prev = snap.playhead;
    const dir: "forward" | "backward" =
      clamped >= prev ? "forward" : "backward";
    clearTimers();
    // Scrubbing always cancels animation and jumps instantly. The renderer
    // applies reverse-decay visually based on lastScrubDir.
    snap = {
      currentTarget: clamped - 1,
      playhead: clamped,
      edgeProgress: 0,
      traversedEdges: setTraversed(clamped - 1),
      lastScrubDir: dir,
      running: false,
      paused: false,
    };
    emit();
  }

  function reset() {
    clearTimers();
    snap = { ...EMPTY };
    emit();
  }

  function setSpeed(mult: number) {
    const next = Math.max(0.1, Math.min(8, mult));
    if (next === speed) return;
    // If currently animating an edge, rescale the in-flight progress so it
    // doesn't visually jump.
    if (snap.running && !snap.paused && rafId != null) {
      const now = performance.now();
      const elapsed = now - edgeStartTs;
      const oldRemaining = edgeDurMs - elapsed;
      const ratio = speed / next;
      const newRemaining = oldRemaining * ratio;
      edgeDurMs = (edgeDurMs * ratio);
      edgeStartTs = now - (edgeDurMs - newRemaining);
    }
    speed = next;
  }

  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSnapshot: () => snap,
    start,
    stop,
    pause,
    resume,
    toggle,
    scrubTo,
    reset,
    setSpeed,
    setOnArrive(cb) {
      onArrive = cb;
    },
  };
}

/**
 * React binding — subscribes a component to a controller's snapshot.
 * Returns the latest snapshot; will trigger re-render on each emit.
 */
export function useFlowAnim(controller: FlowAnimController): FlowAnimSnapshot {
  return useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
    controller.getSnapshot,
  );
}

export const __test_only = { noop };
