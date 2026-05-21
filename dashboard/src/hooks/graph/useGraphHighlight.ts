/**
 * useGraphHighlight — Jarvis MCP query visualization (epic #1157, Phase 2).
 *
 * Subscribes to useMCPActivity and exposes highlighted node/edge IDs with a
 * 5-second decay timer. GraphCanvas passes highlightedNodeIds as forceVisibleIds
 * (so LoD never drops highlighted nodes) and triggers a selectPoints() pulse.
 *
 * Interface contract (stable across phases):
 *   - highlightedNodeIds  — Set<string> of currently highlighted node IDs
 *   - highlightedEdgeIds  — Set<string> of currently highlighted edge IDs
 *   - source              — 'mcp-query' | 'manual' | null
 *   - isActive            — true while a highlight decay is in progress
 *   - highlight(nodeIds, edgeIds?, durationMs?) — imperative trigger
 *   - clear()             — immediately clear
 *
 * Phase 2 additions:
 *   - agentId             — agent_id from the last event (for color tinting)
 *   - enabled             — toggle the SSE subscription on/off
 *   - latestEvent         — raw MCPActivityEvent for the activity log
 *   - eventLog            — rolling last-50 log for the slide-out panel
 *   - totalCount          — X MCP queries today counter
 *   - replayHighlight(e)  — re-apply the highlight from a historical event
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { useMCPActivity } from './useMCPActivity'
import type { MCPActivityEvent } from './useMCPActivity'

// ── Types ─────────────────────────────────────────────────────────────────────

export type HighlightSource = 'mcp-query' | 'manual' | null

export interface GraphHighlightState {
  /** Node IDs currently highlighted (empty = no active highlight) */
  highlightedNodeIds: ReadonlySet<string>
  /** Edge IDs currently highlighted */
  highlightedEdgeIds: ReadonlySet<string>
  /** Who set the highlight */
  source: HighlightSource
  /** Whether the highlight is actively animating */
  isActive: boolean
  /** agent_id from the last MCP event (Phase 2 color tinting) */
  agentId: string | null
  /** Whether the SSE overlay is enabled */
  enabled: boolean
  /** Raw last event for the activity log panel */
  latestEvent: MCPActivityEvent | null
  /** Rolling log of last 50 events */
  eventLog: MCPActivityEvent[]
  /** Total MCP queries since mount */
  totalCount: number
  /** Whether the SSE stream is connected */
  sseConnected: boolean
}

export interface GraphHighlightControls {
  /**
   * Highlight a set of node + edge IDs temporarily.
   * The highlight decays after `durationMs` (default 5000ms).
   * Calling this while a highlight is active replaces the current one.
   */
  highlight(nodeIds: string[], edgeIds?: string[], durationMs?: number): void
  /** Immediately clear any active highlight. */
  clear(): void
  /** Toggle the MCP activity overlay on/off. */
  setEnabled(enabled: boolean): void
  /** Re-apply the highlight from a historical log entry. */
  replayHighlight(event: MCPActivityEvent): void
}

export type UseGraphHighlightReturn = GraphHighlightState & GraphHighlightControls

// ── Constants ─────────────────────────────────────────────────────────────────

const DEFAULT_DECAY_MS = 5000
const EMPTY_SET = new Set<string>()

// ── Internal state shape ──────────────────────────────────────────────────────

interface HighlightCore {
  nodeIds: ReadonlySet<string>
  edgeIds: ReadonlySet<string>
  source: HighlightSource
  agentId: string | null
  isActive: boolean
}

const INACTIVE: HighlightCore = {
  nodeIds: EMPTY_SET,
  edgeIds: EMPTY_SET,
  source: null,
  agentId: null,
  isActive: false,
}

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useGraphHighlight(): UseGraphHighlightReturn {
  const [enabled, setEnabledState] = useState(true)
  const [highlight_, setHighlight] = useState<HighlightCore>(INACTIVE)
  const decayTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // SSE subscription — disable when overlay is toggled off
  const activity = useMCPActivity(enabled)

  // ── Clear highlight ────────────────────────────────────────────────────────

  const clear = useCallback(() => {
    if (decayTimerRef.current) {
      clearTimeout(decayTimerRef.current)
      decayTimerRef.current = null
    }
    setHighlight(INACTIVE)
  }, [])

  // ── Apply highlight with decay ─────────────────────────────────────────────

  const applyHighlight = useCallback(
    (nodeIds: string[], edgeIds: string[], source: HighlightSource, agentId: string | null, durationMs: number) => {
      if (decayTimerRef.current) clearTimeout(decayTimerRef.current)

      setHighlight({
        nodeIds: new Set(nodeIds),
        edgeIds: new Set(edgeIds),
        source,
        agentId,
        isActive: true,
      })

      decayTimerRef.current = setTimeout(() => {
        setHighlight(INACTIVE)
        decayTimerRef.current = null
      }, durationMs)
    },
    [],
  )

  // ── Imperative highlight (manual/external callers) ─────────────────────────

  const highlight = useCallback(
    (nodeIds: string[], edgeIds: string[] = [], durationMs = DEFAULT_DECAY_MS) => {
      applyHighlight(nodeIds, edgeIds, 'manual', null, durationMs)
    },
    [applyHighlight],
  )

  // ── Replay a historical log entry ─────────────────────────────────────────

  const replayHighlight = useCallback(
    (event: MCPActivityEvent) => {
      applyHighlight(
        event.returned_node_ids ?? [],
        event.returned_edge_ids ?? [],
        'mcp-query',
        event.agent_id ?? null,
        DEFAULT_DECAY_MS,
      )
    },
    [applyHighlight],
  )

  // ── Toggle overlay ────────────────────────────────────────────────────────

  const setEnabled = useCallback(
    (next: boolean) => {
      setEnabledState(next)
      if (!next) clear()
    },
    [clear],
  )

  // ── React to incoming SSE events ──────────────────────────────────────────

  const latestEventRef = useRef<MCPActivityEvent | null>(null)

  useEffect(() => {
    const ev = activity.latestEvent
    if (!ev) return
    // Only trigger if this is a new event (reference changed)
    if (ev === latestEventRef.current) return
    latestEventRef.current = ev

    const nodeIds = ev.returned_node_ids ?? []
    const edgeIds = ev.returned_edge_ids ?? []
    if (nodeIds.length === 0 && edgeIds.length === 0) return

    applyHighlight(nodeIds, edgeIds, 'mcp-query', ev.agent_id ?? null, DEFAULT_DECAY_MS)
  }, [activity.latestEvent, applyHighlight])

  // ── Cleanup on unmount ────────────────────────────────────────────────────

  useEffect(() => {
    return () => {
      if (decayTimerRef.current) clearTimeout(decayTimerRef.current)
    }
  }, [])

  return {
    // Highlight state
    highlightedNodeIds: highlight_.nodeIds,
    highlightedEdgeIds: highlight_.edgeIds,
    source: highlight_.source,
    isActive: highlight_.isActive,
    agentId: highlight_.agentId,
    // Overlay control
    enabled,
    setEnabled,
    // Activity stream data
    sseConnected: activity.connected,
    latestEvent: activity.latestEvent,
    eventLog: activity.eventLog,
    totalCount: activity.totalCount,
    // Controls
    highlight,
    clear,
    replayHighlight,
  }
}
