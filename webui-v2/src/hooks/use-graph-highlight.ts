/* ============================================================
   hooks/use-graph-highlight.ts — Jarvis MCP-query glow (#1157).

   Subscribes to useMCPActivity and exposes the node IDs the most recent MCP
   tool call touched, with a decay timer. The graph canvas reads
   `highlightedNodeIds` (and resolves the incident edges itself, since WebUI v2
   edges are keyed by endpoint, not by a synthetic edge id) and runs a transient
   GLOW/PULSE animation that fades over ~DECAY_MS — a "Jarvis" effect showing the
   agent working through the graph in real time.

   Ported + adapted from the deleted v1 dashboard hook (#1232). Differences:
     • v2 edges have no `id`; the backend's returned_edge_ids are endpoint IDs,
       so we fold them into the highlighted NODE set and let the canvas light any
       edge whose BOTH endpoints are highlighted. This is more robust than
       matching opaque edge ids and needs no Go change.
     • Bursts (multiple tools in quick succession) UNION their nodes within a
       short window so a multi-tool agent step glows as one coherent sweep,
       rather than each event clobbering the last.

   Interface:
     • highlightedNodeIds — Set<string> currently glowing (union of recent events)
     • epoch              — bumps on every new highlight (canvas restarts its rAF)
     • isActive           — true while a glow is decaying
     • enabled / setEnabled — toggle the SSE subscription + glow
     • sseConnected, latestEvent, eventLog, totalCount — activity stream state
     • replay(event)      — re-trigger the glow for a historical log entry
   ============================================================ */

import { useCallback, useEffect, useRef, useState } from "react";
import { useMCPActivity, type MCPActivityEvent } from "./use-mcp-activity";

// ── Constants ─────────────────────────────────────────────────────────────────

/** How long a glow takes to fully fade (ms). Owner spec: ~1–2s. */
export const DECAY_MS = 1800;
/** Bursts within this window UNION into one glow rather than replacing it. */
const BURST_WINDOW_MS = 350;
const EMPTY_SET: ReadonlySet<string> = new Set();

// ── Types ─────────────────────────────────────────────────────────────────────

export interface GraphHighlightState {
  /** Node IDs currently glowing (empty = nothing active). */
  highlightedNodeIds: ReadonlySet<string>;
  /**
   * Monotonic counter bumped on every fresh highlight. The canvas watches this
   * to (re)start its rAF glow loop without re-subscribing to set identity.
   */
  epoch: number;
  /** Whether a glow is currently decaying. */
  isActive: boolean;
  /** agent_id of the latest event (reserved for per-agent tinting). */
  agentId: string | null;
  /** Whether the overlay (and SSE subscription) is enabled. */
  enabled: boolean;
  /** Whether the SSE stream is connected. */
  sseConnected: boolean;
  /** Raw latest event for the activity badge/log. */
  latestEvent: MCPActivityEvent | null;
  /** Rolling last-50 event log. */
  eventLog: MCPActivityEvent[];
  /** Total MCP queries since mount. */
  totalCount: number;
}

export interface GraphHighlightControls {
  /** Toggle the overlay + SSE subscription on/off. */
  setEnabled: (enabled: boolean) => void;
  /** Re-trigger the glow from a historical log entry. */
  replay: (event: MCPActivityEvent) => void;
}

export type UseGraphHighlightReturn = GraphHighlightState & GraphHighlightControls;

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useGraphHighlight(): UseGraphHighlightReturn {
  const [enabled, setEnabledState] = useState(true);
  const [nodeIds, setNodeIds] = useState<ReadonlySet<string>>(EMPTY_SET);
  const [epoch, setEpoch] = useState(0);
  const [agentId, setAgentId] = useState<string | null>(null);

  const decayTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastTriggerRef = useRef(0);

  const activity = useMCPActivity(enabled);

  const clear = useCallback(() => {
    if (decayTimerRef.current) {
      clearTimeout(decayTimerRef.current);
      decayTimerRef.current = null;
    }
    setNodeIds(EMPTY_SET);
    setAgentId(null);
  }, []);

  /**
   * Apply a glow over `ids`. Within BURST_WINDOW_MS of the previous trigger we
   * UNION (a multi-tool agent step glows as one sweep); otherwise we replace.
   */
  const applyGlow = useCallback((ids: string[], agent: string | null) => {
    if (ids.length === 0) return;
    const now = Date.now();
    const burst = now - lastTriggerRef.current < BURST_WINDOW_MS;
    lastTriggerRef.current = now;

    setNodeIds((prev) => {
      const next = new Set(burst ? prev : EMPTY_SET);
      for (const id of ids) next.add(id);
      return next;
    });
    setAgentId(agent);
    // Bump epoch so the canvas (re)starts the rAF glow from full intensity.
    setEpoch((e) => e + 1);

    if (decayTimerRef.current) clearTimeout(decayTimerRef.current);
    decayTimerRef.current = setTimeout(() => {
      setNodeIds(EMPTY_SET);
      setAgentId(null);
      decayTimerRef.current = null;
    }, DECAY_MS);
  }, []);

  const replay = useCallback(
    (event: MCPActivityEvent) => {
      const ids = [
        ...(event.returned_node_ids ?? []),
        // v2: edge ids are endpoint ids — fold into the node set so the canvas
        // lights any edge whose both endpoints are highlighted.
        ...(event.returned_edge_ids ?? []),
      ];
      applyGlow(ids, event.agent_id ?? null);
    },
    [applyGlow],
  );

  const setEnabled = useCallback(
    (next: boolean) => {
      setEnabledState(next);
      if (!next) clear();
    },
    [clear],
  );

  // React to incoming SSE events (reference-change guarded).
  const lastEventRef = useRef<MCPActivityEvent | null>(null);
  useEffect(() => {
    const ev = activity.latestEvent;
    if (!ev || ev === lastEventRef.current) return;
    lastEventRef.current = ev;
    replay(ev);
  }, [activity.latestEvent, replay]);

  useEffect(() => () => {
    if (decayTimerRef.current) clearTimeout(decayTimerRef.current);
  }, []);

  return {
    highlightedNodeIds: nodeIds,
    epoch,
    isActive: nodeIds.size > 0,
    agentId,
    enabled,
    sseConnected: activity.connected,
    latestEvent: activity.latestEvent,
    eventLog: activity.eventLog,
    totalCount: activity.totalCount,
    setEnabled,
    replay,
  };
}
