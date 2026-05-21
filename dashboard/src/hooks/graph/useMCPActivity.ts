/**
 * useMCPActivity — SSE hook for real-time MCP tool call visualization.
 *
 * Streams from GET /api/mcp-activity/stream and publishes each event to
 * subscribers. Mirrors the useIndexProgress SSE pattern.
 *
 * Lifecycle:
 *   - Opens an EventSource when `enabled` is true (default).
 *   - Reconnects automatically on transient errors (browser-native ES behavior).
 *   - Closes and cleans up on component unmount.
 *   - Exposes the last event and a rolling log of the last MAX_LOG events.
 *
 * Phase 2 of epic #1157 — implements the SSE subscription side. GraphCanvas
 * uses the returned `latestEvent` to drive selectPoints() highlighting.
 */

import { useCallback, useEffect, useRef, useState } from 'react'

// ── Types ─────────────────────────────────────────────────────────────────────

/**
 * Wire shape of a single MCP tool call event from /api/mcp-activity/stream.
 * Mirrors internal/mcp.MCPActivityEvent in Go.
 */
export interface MCPActivityEvent {
  tool_name: string
  query_args?: Record<string, unknown>
  returned_node_ids?: string[]
  returned_edge_ids?: string[]
  agent_id?: string
  timestamp: number
}

export interface MCPActivityState {
  /** Whether the SSE connection is open */
  connected: boolean
  /** The most recent event received (null = no events yet) */
  latestEvent: MCPActivityEvent | null
  /** Rolling log of the last MAX_LOG events, newest last */
  eventLog: MCPActivityEvent[]
  /** Count of events received since mount */
  totalCount: number
}

export interface UseMCPActivityReturn extends MCPActivityState {
  /** Clear the event log and reset counters */
  clear: () => void
}

// ── Constants ─────────────────────────────────────────────────────────────────

const SSE_URL = '/api/mcp-activity/stream'
const MAX_LOG = 50

const INITIAL_STATE: MCPActivityState = {
  connected: false,
  latestEvent: null,
  eventLog: [],
  totalCount: 0,
}

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * @param enabled - When false, no EventSource is opened (toggle support).
 *                  Defaults to true so GraphCanvas always subscribes when mounted.
 */
export function useMCPActivity(enabled = true): UseMCPActivityReturn {
  const [state, setState] = useState<MCPActivityState>(INITIAL_STATE)
  const esRef = useRef<EventSource | null>(null)

  const clear = useCallback(() => {
    setState(INITIAL_STATE)
  }, [])

  useEffect(() => {
    if (!enabled) {
      esRef.current?.close()
      esRef.current = null
      setState((prev) => ({ ...prev, connected: false }))
      return
    }

    const es = new EventSource(SSE_URL)
    esRef.current = es

    // ── connected ──────────────────────────────────────────────────────────
    es.addEventListener('connected', () => {
      setState((prev) => ({ ...prev, connected: true }))
    })

    // ── activity (named SSE event from the handler) ────────────────────────
    es.addEventListener('activity', (ev: MessageEvent) => {
      try {
        const event: MCPActivityEvent = JSON.parse(ev.data as string)
        setState((prev) => {
          const log = [...prev.eventLog, event]
          // Keep only last MAX_LOG
          if (log.length > MAX_LOG) log.splice(0, log.length - MAX_LOG)
          return {
            connected: true,
            latestEvent: event,
            eventLog: log,
            totalCount: prev.totalCount + 1,
          }
        })
      } catch {
        // Malformed JSON — ignore.
      }
    })

    // ── heartbeat ──────────────────────────────────────────────────────────
    es.addEventListener('heartbeat', () => {
      setState((prev) => ({ ...prev, connected: true }))
    })

    // ── network error ──────────────────────────────────────────────────────
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setState((prev) => ({ ...prev, connected: false }))
      }
    }

    return () => {
      es.close()
      esRef.current = null
    }
  }, [enabled])

  return { ...state, clear }
}
