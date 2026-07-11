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
     • error     — either (a) the stream dropped AFTER meta (a real
                   mid-stream failure the caller should fall back from to the
                   full-payload fetch, since the shapes are identical), (b)
                   the backend emitted a distinguishable `error` SSE event
                   (a genuine warm/load failure, not just "still warming" —
                   #5722), or (c) the warm retry ceiling was reached with no
                   success (#5722) — `errorMessage` carries a user-facing
                   detail in cases (b)/(c) so the screen can surface it
                   instead of spinning forever.

   EventSource can't read a non-2xx body, so a 503 (cold group) surfaces as
   an `onerror` BEFORE any event. We distinguish that (→ warming, retry) from
   a drop after meta (→ error, caller falls back). #5722: a cold group whose
   warm attempt has genuinely FAILED (not just not-yet-warm) is instead
   signalled via a dedicated `error` SSE event the backend upgrades to (see
   internal/dashboard/v2_graph_stream.go), and — belt and braces — a warm
   that never succeeds at all trips a retry ceiling (see
   lib/graph-stream-warm-policy) rather than retrying forever.
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
import { decideWarmRetry, parseWarmErrorEvent } from "@/lib/graph-stream-warm-policy";

export type GraphStreamPhase = "idle" | "warming" | "streaming" | "done" | "error";

export interface UseGraphStreamResult {
  /** The growing (or complete) normalized payload — render it as it builds. */
  state: GraphStreamState;
  phase: GraphStreamPhase;
  /** Nodes received so far (progress numerator). */
  loadedNodes: number;
  /** Total nodes from `meta` (progress denominator); 0 until meta arrives. */
  totalNodes: number;
  /**
   * User-facing detail for the `error` phase (#5722): the backend's `error`
   * SSE event message, or the retry-ceiling give-up message. Null outside
   * the `error` phase, or for the (generic, caller-falls-back) mid-stream
   * drop case where the fallback fetch's own error is what matters.
   */
  errorMessage: string | null;
}

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
 * @param enabled   When false (e.g. the caller fell back to the full-payload
 *                  fetch), no EventSource is opened and the hook stays idle.
 * @param retryKey  #5722 — bump this (e.g. from a "Retry" button) to force a
 *                  fresh connect cycle after the hook has given up (`error`
 *                  phase), without needing to change `groupId`/`enabled`.
 */
export function useGraphStream(
  groupId: string,
  enabled = true,
  retryKey = 0,
): UseGraphStreamResult {
  const [state, dispatch] = useReducer(reducer, undefined, initialStreamState);
  const [phase, setPhase] = useState<GraphStreamPhase>("idle");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

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
    setErrorMessage(null);

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

    // #5722 — retry with the capped backoff schedule until the retry ceiling
    // (decideWarmRetry) is reached, then GIVE UP: surface `error` with a
    // helpful message instead of retrying forever.
    const scheduleWarmRetry = () => {
      if (cancelled || retryTimer !== null) return;
      const decision = decideWarmRetry(warmAttempt);
      warmAttempt += 1;
      if (decision.kind === "giveUp") {
        setErrorMessage(decision.message);
        setPhase("error");
        phaseRef.current = "error";
        return;
      }
      retryTimer = setTimeout(() => {
        retryTimer = null;
        connect();
      }, decision.delayMs);
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

      conn.onerror = (ev: Event) => {
        if (cancelled) return;
        // A clean close after `done` lands here too — ignore it.
        if (phaseRef.current === "done") return;
        // #5722 — the backend's distinguishable `error` SSE event (a genuine
        // warm/load failure, not just "still warming") ALSO surfaces here:
        // per the EventSource spec, a server "event: error" line dispatches
        // an event of type "error" the same as a native connection failure.
        // The two are distinguished by shape: the server's carries `.data`
        // (it's actually a MessageEvent), a bare connection failure does not.
        if (ev instanceof MessageEvent && typeof ev.data === "string") {
          const detail = parseWarmErrorEvent(ev.data);
          closeES();
          setErrorMessage(detail.message);
          setPhase("error");
          phaseRef.current = "error";
          clearRetry();
          return;
        }
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
  }, [groupId, enabled, retryKey]);

  return {
    state,
    phase,
    loadedNodes: state.payload.nodes.length,
    totalNodes: state.totalNodes,
    errorMessage,
  };
}
