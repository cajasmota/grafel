/* ============================================================
   hooks/use-graph-stream.ts — progressive graph SSE consumer.

   Increment 2 of epic #5446. Streams GET /api/v2/graph/:group/stream
   (SSE: connected → meta → chunk… → done) and accumulates it into the
   SAME normalized GraphPayload the full-payload fetch yields (via the
   pure reducer in lib/graph-stream-reducer), so the Graph screen + the
   cosmos canvas consume the growing graph with NO data-model change and
   build up live instead of a long blank wait.

   Phases
     • idle      — disabled / not started.
     • warming   — the group is cold; the daemon returns 503 and warms in
                   the background. We surface a "warming index…" state and
                   retry with a bounded, capped backoff.
     • streaming — meta received; chunks are arriving, the payload grows,
                   the progress counter advances.
     • done      — `done` received; the payload is complete.
     • error     — the stream dropped AFTER meta (a real mid-stream failure
                   the caller should fall back from to the full-payload
                   fetch, since the shapes are identical).

   EventSource can't read a non-2xx body, so a 503 (cold group) surfaces as
   an `onerror` BEFORE any event. We distinguish that (→ warming, retry)
   from a drop after meta (→ error, caller falls back).
   ============================================================ */

import { useEffect, useReducer, useRef, useState } from "react";
import { api } from "@/lib/api";
import {
  initialStreamState,
  applyMeta,
  applyChunk,
  applyDone,
  type GraphStreamState,
  type GraphStreamMetaWire,
  type GraphStreamChunkWire,
} from "@/lib/graph-stream-reducer";

export type GraphStreamPhase = "idle" | "warming" | "streaming" | "done" | "error";

export interface UseGraphStreamResult {
  /** The growing (or complete) normalized payload — render it as it builds. */
  state: GraphStreamState;
  phase: GraphStreamPhase;
  /** Nodes received so far (progress numerator). */
  loadedNodes: number;
  /** Total nodes from `meta` (progress denominator); 0 until meta arrives. */
  totalNodes: number;
}

// Reconnect/retry backoff for a COLD group (503 → warming). Bounded + capped so
// a slow warm self-heals without hammering the endpoint; clamps to the last
// element and keeps retrying. Mirrors the use-mcp-activity schedule.
const WARM_BACKOFF_MS = [1000, 2000, 3500, 5000];

type Action =
  | { type: "meta"; meta: GraphStreamMetaWire }
  | { type: "chunk"; chunk: GraphStreamChunkWire }
  | { type: "done" }
  | { type: "reset" };

function reducer(state: GraphStreamState, action: Action): GraphStreamState {
  switch (action.type) {
    case "meta":
      return applyMeta(state, action.meta);
    case "chunk":
      return applyChunk(state, action.chunk);
    case "done":
      return applyDone(state);
    case "reset":
      return initialStreamState();
  }
}

/**
 * Consume the progressive graph stream for `groupId`.
 *
 * @param enabled  When false (e.g. the caller fell back to the full-payload
 *                 fetch), no EventSource is opened and the hook stays idle.
 */
export function useGraphStream(groupId: string, enabled = true): UseGraphStreamResult {
  const [state, dispatch] = useReducer(reducer, undefined, initialStreamState);
  const [phase, setPhase] = useState<GraphStreamPhase>("idle");

  // Latest phase in a ref so the long-lived effect's handlers branch on the
  // current phase (warming-vs-mid-stream) without re-subscribing.
  const phaseRef = useRef<GraphStreamPhase>("idle");
  phaseRef.current = phase;

  useEffect(() => {
    if (!enabled || !groupId) {
      setPhase("idle");
      return;
    }

    // Fresh group / re-enable → clear any prior accumulation.
    dispatch({ type: "reset" });
    setPhase("warming");
    phaseRef.current = "warming";

    let cancelled = false;
    let es: EventSource | null = null;
    let warmAttempt = 0;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    // Whether `meta` has been seen on the CURRENT connection — distinguishes a
    // 503 cold-start (error before meta → warm + retry) from a real mid-stream
    // drop (error after meta → surface error so the caller falls back).
    let sawMeta = false;

    const clearRetry = () => {
      if (retryTimer !== null) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
    };

    const closeES = () => {
      if (es) {
        es.onerror = null;
        es.close();
        es = null;
      }
    };

    const scheduleWarmRetry = () => {
      if (cancelled || retryTimer !== null) return;
      const idx = Math.min(warmAttempt, WARM_BACKOFF_MS.length - 1);
      const delay = WARM_BACKOFF_MS[idx];
      warmAttempt += 1;
      retryTimer = setTimeout(() => {
        retryTimer = null;
        connect();
      }, delay);
    };

    const connect = () => {
      if (cancelled) return;
      closeES();
      sawMeta = false;

      const conn = new EventSource(api.graphStreamUrl(groupId));
      es = conn;

      conn.addEventListener("meta", (ev: MessageEvent) => {
        try {
          const meta: GraphStreamMetaWire = JSON.parse(ev.data as string);
          sawMeta = true;
          warmAttempt = 0; // a successful connect resets the warm backoff.
          dispatch({ type: "reset" });
          dispatch({ type: "meta", meta });
          if (!cancelled) {
            setPhase("streaming");
            phaseRef.current = "streaming";
          }
        } catch {
          /* malformed meta — leave the onerror path to handle recovery. */
        }
      });

      conn.addEventListener("chunk", (ev: MessageEvent) => {
        try {
          const chunk: GraphStreamChunkWire = JSON.parse(ev.data as string);
          dispatch({ type: "chunk", chunk });
        } catch {
          /* skip a malformed chunk; the stream continues. */
        }
      });

      conn.addEventListener("done", () => {
        if (cancelled) return;
        dispatch({ type: "done" });
        setPhase("done");
        phaseRef.current = "done";
        clearRetry();
        closeES();
      });

      conn.onerror = () => {
        if (cancelled) return;
        // A clean close after `done` lands here too — ignore it.
        if (phaseRef.current === "done") return;
        closeES();
        if (sawMeta) {
          // Dropped MID-STREAM after meta — a real failure. Surface `error` so
          // the caller falls back to the full-payload fetch (identical shape).
          setPhase("error");
          phaseRef.current = "error";
          clearRetry();
          return;
        }
        // Error BEFORE meta — the group is cold (daemon returned 503 + is
        // warming) or the connection failed early. Stay in the warming state
        // and retry with backoff.
        setPhase("warming");
        phaseRef.current = "warming";
        scheduleWarmRetry();
      };
    };

    connect();

    return () => {
      cancelled = true;
      clearRetry();
      closeES();
    };
  }, [groupId, enabled]);

  return {
    state,
    phase,
    loadedNodes: state.payload.nodes.length,
    totalNodes: state.totalNodes,
  };
}
