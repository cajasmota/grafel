/**
 * MCPActivityOverlay — Jarvis Phase 2 activity log slide-out panel (#1157, #1225).
 *
 * Renders:
 *   1. A "pulse badge" showing the SSE connection state + query count.
 *   2. A slide-out panel listing the last 50 MCP events with timestamp,
 *      tool_name, node count, and a "replay" button that re-highlights
 *      the graph for that query.
 *
 * The panel is triggered by clicking the pulse badge. It can be closed by
 * clicking the badge again or pressing Escape.
 *
 * Does NOT render when `enabled` is false (the sidebar toggle hides it).
 */

import { memo, useState, useEffect, useRef, useCallback } from 'react'
import { Activity, X, RefreshCw } from 'lucide-react'
import type { MCPActivityEvent } from '@/hooks/graph/useMCPActivity'

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatTs(ts: number): string {
  const d = new Date(ts)
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  return `${h}:${m}:${s}`
}

function toolShortName(toolName: string): string {
  // Strip common "archigraph_" prefix for compact display
  return toolName.replace(/^archigraph_/, '')
}

// ── Agent color tinting — deterministic hue per agent_id ─────────────────────

const AGENT_COLORS = [
  '#38bdf8', // sky-400
  '#a78bfa', // violet-400
  '#34d399', // emerald-400
  '#fb923c', // orange-400
  '#f472b6', // pink-400
  '#facc15', // yellow-400
]

function agentColor(agentId: string | null | undefined): string {
  if (!agentId) return AGENT_COLORS[0]
  let h = 0
  for (let i = 0; i < agentId.length; i++) {
    h = ((h << 5) - h + agentId.charCodeAt(i)) | 0
  }
  return AGENT_COLORS[Math.abs(h) % AGENT_COLORS.length]
}

// ── Props ─────────────────────────────────────────────────────────────────────

export interface MCPActivityOverlayProps {
  /** Whether the overlay is enabled (controlled by sidebar toggle) */
  enabled: boolean
  /** Whether SSE is connected to the daemon */
  connected: boolean
  /** Whether a highlight is currently active */
  isActive: boolean
  /** agent_id of the latest active highlight (for color badge) */
  agentId: string | null
  /** Total count of MCP queries since mount */
  totalCount: number
  /** Rolling log of last 50 events */
  eventLog: MCPActivityEvent[]
  /** Called when user clicks "replay" on a log entry */
  onReplay: (event: MCPActivityEvent) => void
  /** Whether the graph is in dark mode */
  isDark?: boolean
}

// ── Component ─────────────────────────────────────────────────────────────────

export const MCPActivityOverlay = memo(function MCPActivityOverlay({
  enabled,
  connected,
  isActive,
  agentId,
  totalCount,
  eventLog,
  onReplay,
  isDark = true,
}: MCPActivityOverlayProps) {
  const [panelOpen, setPanelOpen] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)

  // Close panel on Escape
  useEffect(() => {
    if (!panelOpen) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setPanelOpen(false)
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [panelOpen])

  // Scroll log to bottom when new events arrive
  useEffect(() => {
    if (!panelOpen) return
    const el = panelRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [eventLog.length, panelOpen])

  const handleBadgeClick = useCallback(() => {
    setPanelOpen((prev) => !prev)
  }, [])

  if (!enabled) return null

  const color = agentColor(agentId)
  const pulseColor = isActive ? color : (connected ? '#34d399' : '#64748b')
  const badgeLabel = isActive
    ? 'MCP query active — click to see log'
    : connected
    ? `MCP activity stream connected — ${totalCount} queries`
    : 'MCP activity stream — disconnected'

  return (
    <>
      {/* ── Pulse badge ──────────────────────────────────────────────────── */}
      <button
        type="button"
        aria-label={badgeLabel}
        data-testid="mcp-activity-badge"
        onClick={handleBadgeClick}
        style={{
          position: 'absolute',
          bottom: 36,
          right: 14,
          zIndex: 30,
          display: 'flex',
          alignItems: 'center',
          gap: 5,
          padding: '3px 8px',
          borderRadius: 6,
          border: `1px solid ${pulseColor}44`,
          background: isDark ? 'rgba(2,6,23,0.80)' : 'rgba(248,250,252,0.90)',
          color: pulseColor,
          fontSize: 10,
          fontWeight: 600,
          letterSpacing: '0.04em',
          cursor: 'pointer',
          transition: 'border-color 300ms, color 300ms, box-shadow 300ms',
          boxShadow: isActive ? `0 0 8px ${pulseColor}55` : 'none',
          userSelect: 'none',
        }}
      >
        {/* Animated dot */}
        <span
          aria-hidden
          data-testid="mcp-activity-dot"
          style={{
            width: 6,
            height: 6,
            borderRadius: '50%',
            background: pulseColor,
            flexShrink: 0,
            animation: isActive ? 'mcp-pulse 1.2s ease-in-out infinite' : 'none',
            transition: 'background 300ms',
          }}
        />
        <Activity size={10} aria-hidden />
        {totalCount > 0 ? `${totalCount}` : 'MCP'}
      </button>

      {/* ── CSS keyframe for pulse animation ────────────────────────────── */}
      <style>{`
        @keyframes mcp-pulse {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.4; transform: scale(1.6); }
        }
      `}</style>

      {/* ── Slide-out log panel ──────────────────────────────────────────── */}
      {panelOpen && (
        <div
          role="dialog"
          aria-label="MCP activity log"
          data-testid="mcp-activity-panel"
          style={{
            position: 'absolute',
            bottom: 64,
            right: 14,
            zIndex: 40,
            width: 300,
            maxHeight: 400,
            borderRadius: 8,
            border: isDark ? '1px solid rgba(51,65,85,0.6)' : '1px solid rgba(148,163,184,0.4)',
            background: isDark ? 'rgba(2,6,23,0.95)' : 'rgba(248,250,252,0.97)',
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
            boxShadow: '0 8px 24px rgba(0,0,0,0.3)',
          }}
        >
          {/* Panel header */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              padding: '8px 10px',
              borderBottom: isDark ? '1px solid rgba(51,65,85,0.5)' : '1px solid rgba(148,163,184,0.3)',
              flexShrink: 0,
            }}
          >
            <span
              style={{
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.07em',
                textTransform: 'uppercase',
                color: isDark ? '#94a3b8' : '#475569',
              }}
            >
              MCP Activity Log
            </span>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              {/* Connection indicator */}
              <span
                style={{
                  fontSize: 9,
                  color: connected ? '#34d399' : '#64748b',
                  letterSpacing: '0.04em',
                }}
              >
                {connected ? 'live' : 'offline'}
              </span>
              <button
                type="button"
                aria-label="Close activity log"
                onClick={() => setPanelOpen(false)}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  padding: 2,
                  color: isDark ? '#64748b' : '#94a3b8',
                  display: 'flex',
                  alignItems: 'center',
                }}
              >
                <X size={12} aria-hidden />
              </button>
            </div>
          </div>

          {/* Event list */}
          <div
            ref={panelRef}
            style={{
              overflowY: 'auto',
              flex: 1,
              padding: '4px 0',
            }}
          >
            {eventLog.length === 0 ? (
              <p
                style={{
                  padding: '16px 12px',
                  fontSize: 11,
                  color: isDark ? '#475569' : '#94a3b8',
                  textAlign: 'center',
                }}
              >
                No MCP queries yet.
                <br />
                <span style={{ fontSize: 9, opacity: 0.7 }}>
                  Invoke an MCP tool to see activity here.
                </span>
              </p>
            ) : (
              // Newest events at bottom (chronological order)
              [...eventLog].map((ev, idx) => {
                const color_ = agentColor(ev.agent_id)
                const nodeCount = ev.returned_node_ids?.length ?? 0
                const edgeCount = ev.returned_edge_ids?.length ?? 0
                return (
                  <div
                    key={`${ev.timestamp}-${idx}`}
                    style={{
                      display: 'flex',
                      alignItems: 'flex-start',
                      gap: 6,
                      padding: '5px 10px',
                      borderBottom: isDark
                        ? '1px solid rgba(30,41,59,0.5)'
                        : '1px solid rgba(226,232,240,0.5)',
                      fontSize: 10,
                    }}
                  >
                    {/* Agent color dot */}
                    <span
                      aria-hidden
                      title={ev.agent_id ? `Agent: ${ev.agent_id}` : 'No agent ID'}
                      style={{
                        width: 5,
                        height: 5,
                        borderRadius: '50%',
                        background: color_,
                        flexShrink: 0,
                        marginTop: 3,
                      }}
                    />
                    <div style={{ flex: 1, minWidth: 0 }}>
                      {/* Tool name */}
                      <div
                        style={{
                          fontWeight: 600,
                          color: isDark ? '#e2e8f0' : '#1e293b',
                          whiteSpace: 'nowrap',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                        }}
                        title={ev.tool_name}
                      >
                        {toolShortName(ev.tool_name)}
                      </div>
                      {/* Stats row */}
                      <div
                        style={{
                          color: isDark ? '#64748b' : '#94a3b8',
                          display: 'flex',
                          gap: 6,
                          marginTop: 1,
                        }}
                      >
                        <span>{formatTs(ev.timestamp)}</span>
                        {nodeCount > 0 && <span>{nodeCount}N</span>}
                        {edgeCount > 0 && <span>{edgeCount}E</span>}
                      </div>
                    </div>
                    {/* Replay button */}
                    {(nodeCount > 0 || edgeCount > 0) && (
                      <button
                        type="button"
                        aria-label={`Replay highlight for ${ev.tool_name}`}
                        title="Replay highlight"
                        onClick={() => {
                          onReplay(ev)
                          setPanelOpen(false)
                        }}
                        style={{
                          background: 'none',
                          border: 'none',
                          cursor: 'pointer',
                          padding: 2,
                          color: color_,
                          opacity: 0.7,
                          display: 'flex',
                          alignItems: 'center',
                          flexShrink: 0,
                        }}
                      >
                        <RefreshCw size={10} aria-hidden />
                      </button>
                    )}
                  </div>
                )
              })
            )}
          </div>
        </div>
      )}
    </>
  )
})
