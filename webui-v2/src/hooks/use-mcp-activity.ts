/* ============================================================
   hooks/use-mcp-activity.ts — SSE subscription to the MCP activity stream.

   Ported to WebUI v2 from the (deleted) v1 dashboard Jarvis surface
   (#1157 phases 1+2, originally #1232). The Go backend
   (internal/dashboard/handlers_mcp_activity.go + internal/mcp/activity*.go)
   survives unchanged on main, so this hook simply re-subscribes to the same
   GET /api/mcp-activity/stream SSE endpoint via the Vite /api proxy.

   Lifecycle:
     • #4643 — the activity list NO LONGER persists across reloads and is NOT
       seeded from /history. On mount it starts EMPTY and fills purely from the
       LIVE SSE event stream, so the panel always reflects what is happening NOW
       (a reload no longer resurrects the same stale ~11 events). The replay
       controls operate on this live-captured session.
     • Opens an EventSource when `enabled` is true (default).
     • Reconnects on errors/close with a bounded, capped backoff (1s→2s→5s) so a
       daemon RESTART self-heals instead of freezing the panel. (Browser-native
       ES auto-reconnect does NOT recover once the connection lands in CLOSED.)
       Reconnect does NOT re-seed history — events that arrived while we were
       disconnected are simply missed (the live feed is the source of truth).
     • Closes + cleans up on unmount or when toggled off (no EventSource leaks).
     • Exposes the last event, a rolling log (last MAX_LOG), and a count.

   The graph canvas uses `latestEvent` to drive the Jarvis glow/pulse.
   ============================================================ */

import { useCallback, useEffect, useRef, useState } from "react";

// ── Types ─────────────────────────────────────────────────────────────────────

/**
 * Wire shape of a single MCP tool-call event from /api/mcp-activity/stream.
 * Mirrors internal/mcp.MCPActivityEvent in Go.
 */
export interface MCPActivityEvent {
  tool_name: string;
  query_args?: Record<string, unknown>;
  returned_node_ids?: string[];
  returned_edge_ids?: string[];
  agent_id?: string;
  timestamp: number;
  /**
   * #4643 — RETAINED for type back-compat (the overlay still reads it) but is
   * never set now that history-seeding is removed: every event in the log is a
   * live, this-session capture. Always undefined/false.
   */
  isHistory?: boolean;
}

export interface MCPActivityState {
  /** Whether the SSE connection is open. */
  connected: boolean;
  /** The most recent event received (null = none yet). */
  latestEvent: MCPActivityEvent | null;
  /** Rolling log of the last MAX_LOG events, newest last. */
  eventLog: MCPActivityEvent[];
  /** Count of events received since mount. */
  totalCount: number;
}

export interface UseMCPActivityReturn extends MCPActivityState {
  /** Clear the event log and reset counters. */
  clear: () => void;
}

// ── Constants ─────────────────────────────────────────────────────────────────

const SSE_URL = "/api/mcp-activity/stream";
// #1932: bumped from 50 → 100. Replay-all walks the whole panel and the spec
// calls for "50+ activity entries stay smooth". With the generic flow engine
// driving the comet, 100 entries (a few hundred flattened steps) is fine on
// a typical laptop.
const MAX_LOG = 100;

const INITIAL_STATE: MCPActivityState = {
  connected: false,
  latestEvent: null,
  eventLog: [],
  totalCount: 0,
};

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * @param enabled - When false, no EventSource is opened (toggle support).
 *                  Defaults to true so the canvas subscribes when mounted.
 */
// Reconnect backoff schedule (ms). The EventSource is re-opened with this
// bounded, capped progression so a daemon restart (or any transient SSE drop)
// self-heals without hammering the endpoint. Index is clamped to the last
// element, so it caps at 5s and keeps retrying forever.
const RECONNECT_BACKOFF_MS = [1000, 2000, 5000];

/**
 * Stable de-dupe key for an MCP activity event. The backend SSE/history
 * payloads carry no stable id (see internal/dashboard/handlers_mcp_activity.go),
 * so we synthesize one from timestamp + tool + the returned-node fingerprint.
 * Used so a reconnect re-seed from /history doesn't duplicate rows that are
 * already in the live log.
 */
function eventKey(e: MCPActivityEvent): string {
  const nodes = e.returned_node_ids?.length ?? 0;
  const edges = e.returned_edge_ids?.length ?? 0;
  return `${e.timestamp}|${e.tool_name}|${e.agent_id ?? ""}|${nodes}|${edges}`;
}

export function useMCPActivity(enabled = true): UseMCPActivityReturn {
  const [state, setState] = useState<MCPActivityState>(INITIAL_STATE);
  const esRef = useRef<EventSource | null>(null);

  const clear = useCallback(() => setState(INITIAL_STATE), []);

  // ── Single effect owns the whole connection lifecycle ──────────────────────
  // LIVE SSE subscription + ROBUST RECONNECT. Previously the browser-native
  // EventSource auto-reconnect was relied on, but when the daemon RESTARTS the
  // connection moves to CLOSED and is never re-opened — the panel freezes (e.g.
  // stuck at "9/9") and silently drops every new event. We manage reconnection
  // explicitly with a bounded, capped backoff so any transient drop (or daemon
  // restart) self-heals. #4643 — the log is NOT seeded from /history: it starts
  // empty on mount and fills purely from the live feed, so a reload no longer
  // resurrects stale events. The 100-event cap (MAX_LOG) is enforced on the
  // live-append path.
  useEffect(() => {
    if (!enabled) {
      setState((prev) => ({ ...prev, connected: false }));
      return;
    }

    let cancelled = false;
    let es: EventSource | null = null;
    let reconnectAttempt = 0;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let isFirstConnect = true;
    // #4319 — guard against a backoff-reset busy-loop. If the daemon emits the
    // `connected` event and then the stream drops again immediately (a flapping
    // server), resetting reconnectAttempt to 0 on every `connected` would pin
    // the backoff at its 1s floor and hammer EventSource every
    // ~1s forever. We only reset the backoff once a connection has proven STABLE
    // (stayed open past STABLE_MS), so a flapping daemon decays to the 5s cap
    // instead of a tight reconnect storm.
    const STABLE_MS = 10_000;
    let stableTimer: ReturnType<typeof setTimeout> | null = null;
    const clearStableTimer = () => {
      if (stableTimer !== null) {
        clearTimeout(stableTimer);
        stableTimer = null;
      }
    };

    const clearReconnectTimer = () => {
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };

    const closeES = () => {
      // #4319 — a connection is being torn down; cancel the pending "proven
      // stable" reset so a flapping stream can never reset the backoff.
      clearStableTimer();
      if (es) {
        // Drop handlers before closing so a late error can't schedule a
        // reconnect against a connection we're tearing down (no ES leaks).
        es.onerror = null;
        es.close();
        es = null;
      }
    };

    const scheduleReconnect = () => {
      if (cancelled || reconnectTimer !== null) return;
      const idx = Math.min(reconnectAttempt, RECONNECT_BACKOFF_MS.length - 1);
      const delay = RECONNECT_BACKOFF_MS[idx];
      reconnectAttempt += 1;
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        connect();
      }, delay);
    };

    const connect = () => {
      if (cancelled) return;
      closeES();
      // #4643 — no history seed: the log fills purely from the live SSE feed.

      const conn = new EventSource(SSE_URL);
      es = conn;
      esRef.current = conn;

      conn.addEventListener("connected", () => {
        isFirstConnect = false;
        setState((prev) => ({ ...prev, connected: true }));
        // #4319 — do NOT reset the backoff here. A flapping daemon that emits
        // `connected` then immediately drops would otherwise reset to the 1s
        // floor every cycle and busy-loop fetch(/history)+EventSource. Only
        // reset once the connection has held open past STABLE_MS, so a flapping
        // stream decays to the 5s cap instead of hammering the endpoint.
        clearStableTimer();
        stableTimer = setTimeout(() => {
          stableTimer = null;
          if (!cancelled) reconnectAttempt = 0;
        }, STABLE_MS);
      });

      // The Go backend emits this event as "mcp_activity"
      // (internal/dashboard/handlers_mcp_activity.go). Listening for the wrong
      // name silently drops every event → "No MCP queries yet" / no glow.
      conn.addEventListener("mcp_activity", (ev: MessageEvent) => {
        try {
          const event: MCPActivityEvent = JSON.parse(ev.data as string);
          setState((prev) => {
            // De-dupe: a re-seed racing a live event must not double-count.
            const key = eventKey(event);
            if (prev.eventLog.some((e) => eventKey(e) === key)) {
              return { ...prev, connected: true, latestEvent: event };
            }
            const log = [...prev.eventLog, event];
            if (log.length > MAX_LOG) log.splice(0, log.length - MAX_LOG);
            return {
              connected: true,
              latestEvent: event,
              eventLog: log,
              totalCount: prev.totalCount + 1,
            };
          });
        } catch {
          // Malformed JSON — ignore.
        }
      });

      conn.addEventListener("heartbeat", () => {
        setState((prev) => (prev.connected ? prev : { ...prev, connected: true }));
      });

      conn.onerror = () => {
        // CONNECTING means the browser is already retrying internally — let it.
        // CLOSED (or any drop after a daemon restart) is what the native ES
        // never recovers from, so we drive the reconnect ourselves.
        if (conn.readyState === EventSource.CLOSED) {
          setState((prev) => ({ ...prev, connected: false }));
          closeES();
          scheduleReconnect();
        } else if (!isFirstConnect) {
          // Transient drop on an established connection — mark disconnected so
          // the badge reflects reality; the browser-native retry may still
          // recover, but if it lands in CLOSED we'll take over above.
          setState((prev) => ({ ...prev, connected: false }));
        }
      };
    };

    connect();

    return () => {
      cancelled = true;
      clearReconnectTimer();
      clearStableTimer();
      closeES();
      esRef.current = null;
    };
  }, [enabled]);

  return { ...state, clear };
}
