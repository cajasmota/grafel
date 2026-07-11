/* ============================================================
   lib/graph-stream-warm-policy.ts — pure warm retry/give-up policy +
   backend SSE error-event parsing for the progressive graph stream (#5722).

   Extracted as a pure seam (mirrors graph-stream-reducer) so the two
   decisions the SSE consumer (hooks/use-graph-stream) has to make on a
   pre-meta failure are unit-testable without a live EventSource:

     • decideWarmRetry — given how many warm attempts have already been
       made, either retry with the next backoff delay or GIVE UP with a
       user-facing message once the ceiling is reached. Without a ceiling a
       group whose warm attempt keeps failing (not just "still warming")
       spins the "Warming index…" affordance forever with no way out (#5722).
     • parseWarmErrorEvent — parses the backend's distinguishable `error` SSE
       event (event: error, data: {"code","message"}) into a safe shape, with
       a generic fallback if the payload is malformed.
   ============================================================ */

// Reconnect/retry backoff for a COLD group (503 → warming, or a genuine load
// failure surfaced via the `error` event). Bounded + capped so a slow warm
// self-heals without hammering the endpoint.
export const WARM_BACKOFF_MS = [1000, 2000, 3500, 5000];

// #5722 — retry ceiling. Once this many warm attempts have been made with no
// success, give up and surface an error instead of retrying forever. Chosen
// generously (comfortably longer than any real warm, even a slow one) so a
// genuinely slow-but-eventually-successful warm is never cut short, while a
// warm that is truly stuck or failing repeatedly still resolves to a visible
// error within a bounded, human-scale window.
export const MAX_WARM_ATTEMPTS = 8;

export type WarmDecision =
  | { kind: "retry"; delayMs: number }
  | { kind: "giveUp"; message: string };

/**
 * Decide what to do after the `attempt`-th warm failure (0-indexed: the
 * FIRST failure passes attempt=0). Retries with the capped backoff schedule
 * until MAX_WARM_ATTEMPTS is reached, then gives up with a helpful message.
 */
export function decideWarmRetry(attempt: number): WarmDecision {
  if (attempt >= MAX_WARM_ATTEMPTS) {
    return {
      kind: "giveUp",
      message:
        "The graph is taking too long to load. It may have failed to warm up — try again, or check the daemon.",
    };
  }
  const idx = Math.min(attempt, WARM_BACKOFF_MS.length - 1);
  return { kind: "retry", delayMs: WARM_BACKOFF_MS[idx] };
}

/** Parsed shape of the backend's `error` SSE event (v2GraphStreamError). */
export interface WarmErrorDetail {
  code: string;
  message: string;
}

const GENERIC_WARM_ERROR_MESSAGE = "Could not load the graph.";

/**
 * Parse the backend's `error` SSE event payload (event: error, data:
 * {"code","message"}). Falls back to a generic code/message when the payload
 * is malformed or missing fields, so a parse failure never leaves the caller
 * without a renderable error state.
 */
export function parseWarmErrorEvent(raw: string): WarmErrorDetail {
  try {
    const parsed = JSON.parse(raw) as { code?: unknown; message?: unknown };
    const code = typeof parsed.code === "string" && parsed.code ? parsed.code : "unknown";
    const message =
      typeof parsed.message === "string" && parsed.message
        ? parsed.message
        : GENERIC_WARM_ERROR_MESSAGE;
    return { code, message };
  } catch {
    return { code: "unknown", message: GENERIC_WARM_ERROR_MESSAGE };
  }
}
