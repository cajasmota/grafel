/* ============================================================
   hooks/use-mcp-activity.ts — SSE subscription to the MCP activity stream.

   Ported to WebUI v2 from the (deleted) v1 dashboard Jarvis surface
   (#1157 phases 1+2, originally #1232). The Go backend
   (internal/dashboard/handlers_mcp_activity.go + internal/mcp/activity*.go)
   survives unchanged on main, so this hook simply re-subscribes to the same
   GET /api/mcp-activity/stream SSE endpoint via the Vite /api proxy.

   Lifecycle:
     • On mount, fetches the last HISTORY_LIMIT events from
       /api/mcp-activity/history and seeds the activity list (these are
       marked `isHistory: true` so the UI can render them muted). (#1930)
     • Opens an EventSource when `enabled` is true (default).
     • Reconnects automatically on transient errors (browser-native ES).
     • Closes + cleans up on unmount or when toggled off.
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
  /** True for events seeded from /api/mcp-activity/history on mount (#1930). */
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
const HISTORY_URL = "/api/mcp-activity/history";
// #1932: bumped from 50 → 100. Replay-all walks the whole panel and the spec
// calls for "50+ activity entries stay smooth". With the generic flow engine
// driving the comet, 100 entries (a few hundred flattened steps) is fine on
// a typical laptop.
const MAX_LOG = 100;
// #1930: how many historical events to seed on mount.
const HISTORY_LIMIT = 20;

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
export function useMCPActivity(enabled = true): UseMCPActivityReturn {
  const [state, setState] = useState<MCPActivityState>(INITIAL_STATE);
  const esRef = useRef<EventSource | null>(null);
  // Track the timestamp of the last history event seeded so live events
  // arriving before subscribeAt don't double-count with history.
  const subscribeAtRef = useRef<number>(0);

  const clear = useCallback(() => setState(INITIAL_STATE), []);

  // #1930 — Seed the activity list from /api/mcp-activity/history on mount,
  // BEFORE the SSE stream connects. History items are flagged `isHistory: true`
  // so the UI can render them slightly muted (they don't trigger glow).
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    subscribeAtRef.current = Date.now();
    fetch(`${HISTORY_URL}?limit=${HISTORY_LIMIT}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((body: { events?: MCPActivityEvent[] } | null) => {
        if (cancelled || !body?.events?.length) return;
        const historyEvents: MCPActivityEvent[] = body.events.map((e) => ({
          ...e,
          isHistory: true,
        }));
        setState((prev) => {
          // Don't overwrite live events that may have already arrived.
          if (prev.eventLog.length > 0) return prev;
          const log = historyEvents.slice(-MAX_LOG);
          return {
            ...prev,
            eventLog: log,
          };
        });
      })
      .catch(() => {
        // History fetch failing is non-fatal — SSE stream still works.
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  useEffect(() => {
    if (!enabled) {
      esRef.current?.close();
      esRef.current = null;
      setState((prev) => ({ ...prev, connected: false }));
      return;
    }

    const es = new EventSource(SSE_URL);
    esRef.current = es;

    es.addEventListener("connected", () => {
      setState((prev) => ({ ...prev, connected: true }));
    });

    // The Go backend emits this event as "mcp_activity"
    // (internal/dashboard/handlers_mcp_activity.go). Listening for the wrong
    // name silently drops every event → "No MCP queries yet" / no glow.
    es.addEventListener("mcp_activity", (ev: MessageEvent) => {
      try {
        const event: MCPActivityEvent = JSON.parse(ev.data as string);
        setState((prev) => {
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

    es.addEventListener("heartbeat", () => {
      setState((prev) => (prev.connected ? prev : { ...prev, connected: true }));
    });

    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setState((prev) => ({ ...prev, connected: false }));
      }
    };

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [enabled]);

  return { ...state, clear };
}
