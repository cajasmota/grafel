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
  //
  // In the legacy edge-comet engine (createFlowAnim) a "step" is one node
  // arrival between two consecutive hits. In the two-phase call engine
  // (createCallFlowAnim, #1953) a "step" is ONE MCP CALL — the arrow sweeps
  // the entire returned-node polyline in a single ~200ms motion, then all
  // nodes glow synchronously.
  currentTarget: number;
  // Number of steps the playhead is at (0 = entry only, N = full chain).
  // Equivalent to "steps completed".
  playhead: number;
  // 0..1 progress along the current edge OR (in the #1953 call engine) the
  // current polyline sweep when phase === "sweep". 0 outside Phase 1.
  edgeProgress: number;
  // Set of step indices (i = target step index) currently considered
  // "traversed" (tinted). Exposed as a sorted array for renderer use.
  traversedEdges: number[];
  // Latest scrub direction — "forward" or "backward". Lets the renderer
  // decide whether to fade newly-removed edges (reverse decay).
  lastScrubDir: "forward" | "backward" | null;
  // True if the engine is currently animating an edge (not paused / idle).
  running: boolean;
  // True if user paused mid-flow.
  paused: boolean;
  // #1953 two-phase call engine fields (legacy edge engine leaves these unset).
  //   "idle"   no step in flight
  //   "sweep"  Phase 1 — phantom arrow sweeps the polyline (uses edgeProgress)
  //   "glow"   Phase 2 — every returned node pulses simultaneously (uses
  //            glowProgress: 0 at burst start, 1 at full decay)
  //   "gap"    brief settle before the next call kicks off
  phase?: "idle" | "sweep" | "glow" | "gap";
  // 0..1 progress through the Phase 2 glow burst. Only meaningful when
  // phase === "glow". Goes 0 → 1 over the glow duration; the renderer
  // interpolates size/opacity from this.
  glowProgress?: number;
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

// ─────────────────────────────────────────────────────────────────────────────
// #1953 — Two-phase CALL flow animation.
//
// Step model: step = ONE MCP CALL (not one edge between two hits).
//   Phase 1 (sweep, ~200ms): the phantom arrow head linearly interpolates
//     along the ENTIRE polyline of returned nodes for that call. The renderer
//     samples positions along the polyline; the engine just exports a scalar
//     0..1 progress (snap.edgeProgress).
//   Phase 2 (glow,  ~300ms): every returned node pulses simultaneously.
//     The engine exports `glowProgress` 0..1 for the renderer.
//   Inter-step gap (~150ms): brief settle before the next call kicks off.
//
// Total per call at 1× ≈ 650ms (200 + 300 + 150). Speed-multiplier scales
// all three phases.
//
// `setOnArrive(cb)` fires once per call at the START of Phase 2 (so audio
// blips on glow burst, not per-node arrival).
//
// Reduced-motion: Phase 1 is skipped, Phase 2 fires instantly without
// size animation (renderer holds glowProgress = 0 then 1 in one tick).
// ─────────────────────────────────────────────────────────────────────────────

export type CreateCallOpts = {
  // Number of MCP calls in the timeline (NOT flattened node count).
  totalCalls: number;
  // Phase durations (ms at speed=1). Defaults match #1953 spec.
  sweepMs?: number;     // default 200
  glowMs?: number;      // default 300
  interCallMs?: number; // default 150
  speed?: number;       // default 1
  reducedMotion?: boolean;
};

const EMPTY_CALL: FlowAnimSnapshot = {
  currentTarget: -1,
  playhead: 0,
  edgeProgress: 0,
  traversedEdges: [],
  lastScrubDir: null,
  running: false,
  paused: false,
  phase: "idle",
  glowProgress: 0,
};

export function createCallFlowAnim(opts: CreateCallOpts): FlowAnimController {
  const {
    totalCalls,
    sweepMs = 200,
    glowMs = 300,
    interCallMs = 150,
    reducedMotion = false,
  } = opts;
  let speed = opts.speed ?? 1;
  let onArrive: ((stepIdx: number) => void) | null = null;

  let snap: FlowAnimSnapshot = { ...EMPTY_CALL };
  const listeners = new Set<() => void>();

  let rafId: number | null = null;
  let phaseStartTs = 0;
  let phaseDurMs = 0;
  let phaseProgressFrozen = 0;
  let gapTimer: ReturnType<typeof setTimeout> | null = null;

  function emit() { listeners.forEach((l) => l()); }

  function clearTimers() {
    if (rafId != null) { cancelAnimationFrame(rafId); rafId = null; }
    if (gapTimer != null) { clearTimeout(gapTimer); gapTimer = null; }
  }

  function setTraversed(upTo: number): number[] {
    const arr: number[] = [];
    for (let i = 0; i <= upTo; i++) arr.push(i);
    return arr;
  }

  function tickSweep(ts: number) {
    if (snap.paused || !snap.running) return;
    const elapsed = ts - phaseStartTs;
    const p = Math.min(1, elapsed / Math.max(1, phaseDurMs));
    snap = { ...snap, edgeProgress: p };
    emit();
    if (p >= 1) {
      beginGlow();
      return;
    }
    rafId = requestAnimationFrame(tickSweep);
  }

  function tickGlow(ts: number) {
    if (snap.paused || !snap.running) return;
    const elapsed = ts - phaseStartTs;
    const p = Math.min(1, elapsed / Math.max(1, phaseDurMs));
    snap = { ...snap, glowProgress: p };
    emit();
    if (p >= 1) {
      const arrived = snap.currentTarget;
      snap = {
        ...snap,
        phase: "gap",
        glowProgress: 1,
        edgeProgress: 0,
        playhead: arrived + 1,
        traversedEdges: setTraversed(arrived),
      };
      emit();
      // gap then next call (or stop)
      if (arrived < totalCalls - 1) {
        gapTimer = setTimeout(() => {
          gapTimer = null;
          beginCall(arrived + 1);
        }, interCallMs / Math.max(0.0001, speed));
      } else {
        snap = { ...snap, running: false, phase: "idle" };
        rafId = null;
        emit();
      }
      return;
    }
    rafId = requestAnimationFrame(tickGlow);
  }

  function beginGlow() {
    const arrived = snap.currentTarget;
    phaseDurMs = glowMs / Math.max(0.0001, speed);
    phaseStartTs = performance.now();
    phaseProgressFrozen = 0;
    snap = {
      ...snap,
      phase: "glow",
      edgeProgress: 1, // sweep complete
      glowProgress: 0,
    };
    emit();
    // Fire onArrive at START of Phase 2 (audio "result landed" blip).
    onArrive?.(arrived);
    if (reducedMotion) {
      // Instant glow without animation — jump to fully-decayed state.
      snap = {
        ...snap,
        phase: "gap",
        glowProgress: 1,
        edgeProgress: 0,
        playhead: arrived + 1,
        traversedEdges: setTraversed(arrived),
      };
      emit();
      if (arrived < totalCalls - 1) {
        gapTimer = setTimeout(() => {
          gapTimer = null;
          beginCall(arrived + 1);
        }, interCallMs / Math.max(0.0001, speed));
      } else {
        snap = { ...snap, running: false, phase: "idle" };
        emit();
      }
      return;
    }
    rafId = requestAnimationFrame(tickGlow);
  }

  function beginCall(callIdx: number) {
    if (callIdx < 0 || callIdx >= totalCalls) {
      snap = { ...snap, running: false, phase: "idle" };
      emit();
      return;
    }
    snap = {
      ...snap,
      currentTarget: callIdx,
      edgeProgress: 0,
      glowProgress: 0,
      phase: "sweep",
      running: true,
      paused: false,
    };
    emit();
    if (reducedMotion) {
      // Skip Phase 1 entirely; jump straight to Phase 2 glow burst.
      beginGlow();
      return;
    }
    phaseDurMs = sweepMs / Math.max(0.0001, speed);
    phaseStartTs = performance.now();
    phaseProgressFrozen = 0;
    rafId = requestAnimationFrame(tickSweep);
  }

  function start() {
    if (totalCalls < 1) return;
    clearTimers();
    snap = {
      currentTarget: -1,
      playhead: 0,
      edgeProgress: 0,
      traversedEdges: [],
      lastScrubDir: "forward",
      running: true,
      paused: false,
      phase: "idle",
      glowProgress: 0,
    };
    emit();
    beginCall(0);
  }

  function stop() {
    clearTimers();
    snap = {
      ...snap,
      running: false,
      paused: false,
      edgeProgress: 0,
      glowProgress: 0,
      phase: "idle",
    };
    emit();
  }

  function pause() {
    if (!snap.running || snap.paused) return;
    phaseProgressFrozen =
      snap.phase === "sweep" ? (snap.edgeProgress ?? 0) :
      snap.phase === "glow" ? (snap.glowProgress ?? 0) : 0;
    if (rafId != null) { cancelAnimationFrame(rafId); rafId = null; }
    if (gapTimer != null) { clearTimeout(gapTimer); gapTimer = null; }
    snap = { ...snap, paused: true };
    emit();
  }

  function resume() {
    if (!snap.paused) return;
    const offset = phaseProgressFrozen * phaseDurMs;
    phaseStartTs = performance.now() - offset;
    snap = { ...snap, paused: false };
    emit();
    if (snap.phase === "sweep") rafId = requestAnimationFrame(tickSweep);
    else if (snap.phase === "glow") rafId = requestAnimationFrame(tickGlow);
    else if (snap.phase === "gap") {
      // Resume gap as if just-finished glow; queue next call.
      const arrived = snap.currentTarget;
      if (arrived < totalCalls - 1) {
        gapTimer = setTimeout(() => {
          gapTimer = null;
          beginCall(arrived + 1);
        }, interCallMs / Math.max(0.0001, speed));
      } else {
        snap = { ...snap, running: false, phase: "idle" };
        emit();
      }
    }
  }

  function toggle() {
    if (!snap.running && !snap.paused && snap.playhead < totalCalls) {
      if (snap.playhead > 0 && snap.playhead < totalCalls) {
        snap = { ...snap, running: true, paused: false };
        emit();
        beginCall(snap.playhead);
        return;
      }
      start();
      return;
    }
    if (snap.running && !snap.paused) pause();
    else if (snap.paused) resume();
  }

  function scrubTo(target: number) {
    const clamped = Math.max(0, Math.min(totalCalls, target));
    const prev = snap.playhead;
    const dir: "forward" | "backward" = clamped >= prev ? "forward" : "backward";
    clearTimers();
    snap = {
      currentTarget: clamped - 1,
      playhead: clamped,
      edgeProgress: 0,
      glowProgress: 0,
      traversedEdges: setTraversed(clamped - 1),
      lastScrubDir: dir,
      running: false,
      paused: false,
      phase: "idle",
    };
    emit();
  }

  function reset() {
    clearTimers();
    snap = { ...EMPTY_CALL };
    emit();
  }

  function setSpeed(mult: number) {
    const next = Math.max(0.1, Math.min(16, mult));
    if (next === speed) return;
    if (snap.running && !snap.paused && rafId != null) {
      const now = performance.now();
      const elapsed = now - phaseStartTs;
      const oldRemaining = phaseDurMs - elapsed;
      const ratio = speed / next;
      const newRemaining = oldRemaining * ratio;
      phaseDurMs = phaseDurMs * ratio;
      phaseStartTs = now - (phaseDurMs - newRemaining);
    }
    speed = next;
  }

  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSnapshot: () => snap,
    start, stop, pause, resume, toggle, scrubTo, reset, setSpeed,
    setOnArrive(cb) { onArrive = cb; },
  };
}

export const __test_only = { noop };
