/**
 * MCP Activity Log — /mcp-activity
 *
 * Surface 12: Visualise what MCP agents have queried in real time.
 * Each row shows: timestamp, tool_name, args summary, returned node/edge
 * counts, agent_id (colour-tinted).  Clicking a row navigates to
 * /graph/{group}?highlight=<comma-separated node IDs>.
 *
 * Backend:
 *   GET  /api/mcp-activity/history?limit=100  — seed data from JSONL log
 *   GET  /api/mcp-activity/stream             — SSE live tail
 *
 * Closes #1226, part of epic #1157.
 */

import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  fetchMCPActivityHistory,
  subscribeMCPActivityStream,
} from '@/api/client'
import type { MCPActivityEvent } from '@/types/api'
import {
  Activity, Download, RefreshCw, X, Search,
  Zap, Clock, ChevronDown, ChevronRight,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Agent colour palette — up to 12 distinct agents, then wraps.
// Colours are chosen to work in both light and dark themes.
// ─────────────────────────────────────────────────────────────────────────────

const AGENT_COLORS = [
  { bg: 'bg-sky-100 dark:bg-sky-950/50',     text: 'text-sky-700 dark:text-sky-300',     dot: 'bg-sky-400' },
  { bg: 'bg-violet-100 dark:bg-violet-950/50', text: 'text-violet-700 dark:text-violet-300', dot: 'bg-violet-400' },
  { bg: 'bg-emerald-100 dark:bg-emerald-950/50', text: 'text-emerald-700 dark:text-emerald-300', dot: 'bg-emerald-400' },
  { bg: 'bg-amber-100 dark:bg-amber-950/50',  text: 'text-amber-700 dark:text-amber-300',  dot: 'bg-amber-400' },
  { bg: 'bg-rose-100 dark:bg-rose-950/50',    text: 'text-rose-700 dark:text-rose-300',    dot: 'bg-rose-400' },
  { bg: 'bg-cyan-100 dark:bg-cyan-950/50',    text: 'text-cyan-700 dark:text-cyan-300',    dot: 'bg-cyan-400' },
  { bg: 'bg-orange-100 dark:bg-orange-950/50', text: 'text-orange-700 dark:text-orange-300', dot: 'bg-orange-400' },
  { bg: 'bg-pink-100 dark:bg-pink-950/50',    text: 'text-pink-700 dark:text-pink-300',    dot: 'bg-pink-400' },
  { bg: 'bg-teal-100 dark:bg-teal-950/50',    text: 'text-teal-700 dark:text-teal-300',    dot: 'bg-teal-400' },
  { bg: 'bg-indigo-100 dark:bg-indigo-950/50', text: 'text-indigo-700 dark:text-indigo-300', dot: 'bg-indigo-400' },
  { bg: 'bg-lime-100 dark:bg-lime-950/50',    text: 'text-lime-700 dark:text-lime-300',    dot: 'bg-lime-400' },
  { bg: 'bg-fuchsia-100 dark:bg-fuchsia-950/50', text: 'text-fuchsia-700 dark:text-fuchsia-300', dot: 'bg-fuchsia-400' },
]

function agentColor(agentId: string | undefined) {
  if (!agentId) return { bg: 'bg-slate-100 dark:bg-slate-800', text: 'text-slate-500 dark:text-slate-400', dot: 'bg-slate-400' }
  // stable hash for consistent colours across re-renders
  let h = 0
  for (let i = 0; i < agentId.length; i++) h = (h * 31 + agentId.charCodeAt(i)) >>> 0
  return AGENT_COLORS[h % AGENT_COLORS.length]
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function fmtTime(tsMs: number): string {
  const d = new Date(tsMs)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function fmtDate(tsMs: number): string {
  const d = new Date(tsMs)
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

function relMs(tsMs: number): string {
  const diff = Date.now() - tsMs
  if (diff < 5_000) return 'just now'
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  return `${Math.floor(diff / 3_600_000)}h ago`
}

function argsPreview(args: Record<string, unknown> | undefined): string {
  if (!args || Object.keys(args).length === 0) return '—'
  return Object.entries(args)
    .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
    .join(', ')
    .slice(0, 120)
}

// ─────────────────────────────────────────────────────────────────────────────
// Filter chip component
// ─────────────────────────────────────────────────────────────────────────────

interface ChipProps {
  label: string
  active: boolean
  onClick: () => void
  color?: { bg: string; text: string; dot: string }
}

function FilterChip({ label, active, onClick, color }: ChipProps) {
  const base = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-xs font-medium transition-colors cursor-pointer select-none'
  const cls = active
    ? `${base} ${color?.bg ?? 'bg-sky-100 dark:bg-sky-950/50'} ${color?.text ?? 'text-sky-700 dark:text-sky-300'} border-sky-300 dark:border-sky-700`
    : `${base} bg-transparent text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-500`
  return (
    <button type="button" className={cls} onClick={onClick} aria-pressed={active}>
      {color && active && <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${color.dot}`} />}
      {label}
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tool stats panel
// ─────────────────────────────────────────────────────────────────────────────

function ToolStats({ events }: { events: MCPActivityEvent[] }) {
  const stats = useMemo(() => {
    const counts: Record<string, number> = {}
    const nodeCounts: Record<string, number> = {}
    for (const e of events) {
      counts[e.tool_name] = (counts[e.tool_name] ?? 0) + 1
      nodeCounts[e.tool_name] = (nodeCounts[e.tool_name] ?? 0) + (e.returned_node_ids?.length ?? 0)
    }
    return Object.entries(counts)
      .map(([tool, calls]) => ({ tool, calls, nodes: nodeCounts[tool] ?? 0 }))
      .sort((a, b) => b.calls - a.calls)
      .slice(0, 8)
  }, [events])

  if (stats.length === 0) return null

  const maxCalls = stats[0]?.calls ?? 1

  return (
    <section
      className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4"
      data-testid="tool-stats-panel"
    >
      <h2 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider mb-3">
        Tool usage
      </h2>
      <div className="space-y-2">
        {stats.map(({ tool, calls, nodes }) => (
          <div key={tool}>
            <div className="flex items-center justify-between text-xs mb-0.5">
              <span className="font-mono text-slate-700 dark:text-slate-300 truncate max-w-[60%]" title={tool}>
                {tool.replace('archigraph_', '')}
              </span>
              <span className="text-slate-400 tabular-nums">
                {calls} call{calls !== 1 ? 's' : ''} · {nodes} nodes
              </span>
            </div>
            <div className="h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
              <div
                className="h-full rounded-full bg-sky-400 dark:bg-sky-500 transition-all"
                style={{ width: `${(calls / maxCalls) * 100}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Event row
// ─────────────────────────────────────────────────────────────────────────────

interface EventRowProps {
  event: MCPActivityEvent
  group: string
  isNew?: boolean
  onReplay: (nodeIds: string[]) => void
}

function EventRow({ event, group, isNew, onReplay }: EventRowProps) {
  const [expanded, setExpanded] = useState(false)
  const color = agentColor(event.agent_id)
  const nodeCount = event.returned_node_ids?.length ?? 0
  const edgeCount = event.returned_edge_ids?.length ?? 0
  const hasNodes = nodeCount > 0
  const navigate = useNavigate()

  function handleRowClick() {
    if (!hasNodes) return
    onReplay(event.returned_node_ids!)
    navigate(`/graph/${group}?highlight=${event.returned_node_ids!.join(',')}`)
  }

  return (
    <div
      className={[
        'rounded-lg border transition-all',
        isNew
          ? 'border-sky-300 dark:border-sky-600 bg-sky-50/60 dark:bg-sky-950/30 animate-pulse-once'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900',
      ].join(' ')}
      data-testid="activity-row"
    >
      {/* Main row */}
      <div
        className={[
          'flex items-start gap-3 px-4 py-3',
          hasNodes ? 'cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/50' : '',
        ].join(' ')}
        onClick={handleRowClick}
        role={hasNodes ? 'button' : undefined}
        tabIndex={hasNodes ? 0 : undefined}
        onKeyDown={(e) => { if (hasNodes && (e.key === 'Enter' || e.key === ' ')) handleRowClick() }}
        aria-label={hasNodes ? `Replay ${event.tool_name} on graph` : undefined}
      >
        {/* Agent dot */}
        <span className={`mt-1.5 w-2 h-2 rounded-full flex-shrink-0 ${color.dot}`} />

        {/* Tool name */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="text-sm font-mono font-medium text-slate-900 dark:text-slate-100 truncate"
              data-testid="tool-name"
            >
              {event.tool_name}
            </span>

            {/* Agent badge */}
            {event.agent_id && (
              <span className={`px-2 py-0.5 rounded-full text-xs font-medium border ${color.bg} ${color.text} border-transparent`}>
                {event.agent_id}
              </span>
            )}

            {/* Counts */}
            {nodeCount > 0 && (
              <span className="text-xs text-slate-400 tabular-nums">{nodeCount} node{nodeCount !== 1 ? 's' : ''}</span>
            )}
            {edgeCount > 0 && (
              <span className="text-xs text-slate-400 tabular-nums">{edgeCount} edge{edgeCount !== 1 ? 's' : ''}</span>
            )}

            {/* Replay hint */}
            {hasNodes && (
              <span className="text-xs text-sky-500 dark:text-sky-400 ml-auto flex-shrink-0 hidden sm:inline">
                → replay on graph
              </span>
            )}
          </div>

          {/* Args preview */}
          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 font-mono truncate">
            {argsPreview(event.query_args)}
          </p>
        </div>

        {/* Timestamp + expand */}
        <div className="flex items-center gap-2 flex-shrink-0">
          <time
            className="text-xs text-slate-400 tabular-nums"
            dateTime={new Date(event.timestamp).toISOString()}
            title={fmtDate(event.timestamp)}
          >
            {fmtTime(event.timestamp)}
          </time>
          <button
            type="button"
            aria-label={expanded ? 'Collapse details' : 'Expand details'}
            className="p-0.5 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
            onClick={(e) => { e.stopPropagation(); setExpanded((v) => !v) }}
          >
            {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      {/* Expanded detail */}
      {expanded && (
        <div
          className="px-4 pb-3 border-t border-slate-100 dark:border-slate-800 pt-3 space-y-3"
          data-testid="activity-row-detail"
        >
          {/* Full args */}
          {event.query_args && Object.keys(event.query_args).length > 0 && (
            <div>
              <p className="text-xs font-semibold text-slate-500 dark:text-slate-400 mb-1">Arguments</p>
              <pre className="text-xs font-mono bg-slate-50 dark:bg-slate-950 rounded p-2 overflow-x-auto text-slate-700 dark:text-slate-300">
                {JSON.stringify(event.query_args, null, 2)}
              </pre>
            </div>
          )}

          {/* Returned node IDs */}
          {nodeCount > 0 && (
            <div>
              <p className="text-xs font-semibold text-slate-500 dark:text-slate-400 mb-1">
                Returned nodes ({nodeCount})
              </p>
              <div className="flex flex-wrap gap-1">
                {event.returned_node_ids!.slice(0, 20).map((id) => (
                  <span
                    key={id}
                    className="px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-xs font-mono text-slate-600 dark:text-slate-400"
                  >
                    {id}
                  </span>
                ))}
                {nodeCount > 20 && (
                  <span className="text-xs text-slate-400">+{nodeCount - 20} more</span>
                )}
              </div>
            </div>
          )}

          {/* Full timestamp + replay */}
          <div className="flex items-center justify-between">
            <time className="text-xs text-slate-400" dateTime={new Date(event.timestamp).toISOString()}>
              {fmtDate(event.timestamp)} · {relMs(event.timestamp)}
            </time>
            {hasNodes && (
              <button
                type="button"
                onClick={handleRowClick}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-sky-100 dark:bg-sky-950/50 text-sky-700 dark:text-sky-300 hover:bg-sky-200 dark:hover:bg-sky-900/60 transition-colors"
                data-testid="replay-btn"
              >
                <Zap className="w-3 h-3" />
                Replay on graph
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main route
// ─────────────────────────────────────────────────────────────────────────────

const GROUP_DEFAULT = 'fixture-a'

export function MCPActivityRoute() {
  const { group = GROUP_DEFAULT } = useParams()
  const navigate = useNavigate()

  // ── State ──────────────────────────────────────────────────────────────────
  const [liveEvents, setLiveEvents] = useState<MCPActivityEvent[]>([])
  const [newIds, setNewIds] = useState<Set<number>>(new Set())
  const [liveTail, setLiveTail] = useState(true)
  const [filterTool, setFilterTool] = useState<string | null>(null)
  const [filterAgent, setFilterAgent] = useState<string | null>(null)
  const [searchQ, setSearchQ] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)
  const cleanupRef = useRef<(() => void) | null>(null)

  // ── History query ──────────────────────────────────────────────────────────
  const { data: historyData, isLoading, refetch } = useQuery({
    queryKey: ['mcp-activity-history'],
    queryFn: () => fetchMCPActivityHistory(100),
    staleTime: 0,
  })

  const historyEvents: MCPActivityEvent[] = historyData?.events ?? []

  // ── Merge history + live events (deduplicate by timestamp+tool) ────────────
  const allEvents = useMemo(() => {
    const combined = [...historyEvents]
    for (const e of liveEvents) {
      // Simple dedup: skip if same tool + timestamp already in history
      const dup = combined.some((h) => h.timestamp === e.timestamp && h.tool_name === e.tool_name)
      if (!dup) combined.push(e)
    }
    return combined.sort((a, b) => b.timestamp - a.timestamp) // newest first
  }, [historyEvents, liveEvents])

  // ── Live SSE subscription ──────────────────────────────────────────────────
  useEffect(() => {
    if (!liveTail) {
      cleanupRef.current?.()
      cleanupRef.current = null
      return
    }
    const cleanup = subscribeMCPActivityStream(
      (event) => {
        setLiveEvents((prev) => [...prev, event])
        setNewIds((prev) => {
          const next = new Set(prev)
          next.add(event.timestamp)
          setTimeout(() => setNewIds((s) => { const n = new Set(s); n.delete(event.timestamp); return n }), 2_000)
          return next
        })
        if (liveTail) {
          setTimeout(() => bottomRef.current?.scrollIntoView({ behavior: 'smooth' }), 100)
        }
      },
      undefined,
      undefined,
    )
    cleanupRef.current = cleanup
    return () => { cleanup(); cleanupRef.current = null }
  }, [liveTail])

  // ── Derived filter lists ───────────────────────────────────────────────────
  const toolNames = useMemo(() => {
    const s = new Set(allEvents.map((e) => e.tool_name))
    return [...s].sort()
  }, [allEvents])

  const agentIds = useMemo(() => {
    const s = new Set(allEvents.map((e) => e.agent_id).filter(Boolean) as string[])
    return [...s].sort()
  }, [allEvents])

  // ── Filtered events ────────────────────────────────────────────────────────
  const filteredEvents = useMemo(() => {
    let events = allEvents
    if (filterTool) events = events.filter((e) => e.tool_name === filterTool)
    if (filterAgent) events = events.filter((e) => e.agent_id === filterAgent)
    if (searchQ.trim()) {
      const q = searchQ.toLowerCase()
      events = events.filter((e) =>
        e.tool_name.toLowerCase().includes(q) ||
        (e.agent_id ?? '').toLowerCase().includes(q) ||
        argsPreview(e.query_args).toLowerCase().includes(q)
      )
    }
    return events
  }, [allEvents, filterTool, filterAgent, searchQ])

  // ── Export ─────────────────────────────────────────────────────────────────
  const handleExport = useCallback(() => {
    const json = JSON.stringify(filteredEvents, null, 2)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `mcp-activity-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [filteredEvents])

  const handleReplay = useCallback((_nodeIds: string[]) => {
    // Navigation happens inside EventRow — this is a hook for future extensions
  }, [])

  // ─────────────────────────────────────────────────────────────────────────
  return (
    <div className="h-full overflow-y-auto" data-testid="mcp-activity-page">
      <div className="max-w-5xl mx-auto px-4 sm:px-6 py-6 space-y-4">

        {/* ── Header ─────────────────────────────────────────────────────── */}
        <div className="flex items-center gap-3 flex-wrap">
          <Activity className="w-5 h-5 text-slate-500 flex-shrink-0" />
          <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
            MCP Activity
          </h1>
          <span className="text-xs text-slate-400 tabular-nums">
            {filteredEvents.length} event{filteredEvents.length !== 1 ? 's' : ''}
          </span>

          <div className="ml-auto flex items-center gap-2">
            {/* Live tail toggle */}
            <button
              type="button"
              onClick={() => setLiveTail((v) => !v)}
              title={liveTail ? 'Pause live tail' : 'Resume live tail'}
              className={[
                'inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium transition-colors',
                liveTail
                  ? 'bg-emerald-100 dark:bg-emerald-950/50 text-emerald-700 dark:text-emerald-300 border border-emerald-300 dark:border-emerald-700'
                  : 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 border border-slate-200 dark:border-slate-700',
              ].join(' ')}
              data-testid="live-tail-toggle"
            >
              <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${liveTail ? 'bg-emerald-500 animate-pulse' : 'bg-slate-400'}`} />
              {liveTail ? 'Live' : 'Paused'}
            </button>

            {/* Refresh */}
            <button
              type="button"
              onClick={() => refetch()}
              title="Refresh history"
              className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
              data-testid="refresh-btn"
            >
              <RefreshCw className="w-4 h-4" />
            </button>

            {/* Export */}
            <button
              type="button"
              onClick={handleExport}
              title="Export visible rows as JSON"
              className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
              data-testid="export-btn"
            >
              <Download className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* ── Filters row ────────────────────────────────────────────────── */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Search */}
          <div className="relative flex-1 min-w-[160px] max-w-xs">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
            <input
              type="text"
              value={searchQ}
              onChange={(e) => setSearchQ(e.target.value)}
              placeholder="Search tool, agent, args…"
              className="w-full pl-8 pr-3 py-1.5 rounded-lg bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 text-sm text-slate-900 dark:text-slate-100 placeholder-slate-400 outline-none focus:ring-2 focus:ring-sky-400/50"
              data-testid="search-input"
            />
            {searchQ && (
              <button
                type="button"
                onClick={() => setSearchQ('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>

          {/* Divider */}
          {toolNames.length > 0 && (
            <span className="text-slate-300 dark:text-slate-700">|</span>
          )}

          {/* Tool chips */}
          {toolNames.map((tool) => (
            <FilterChip
              key={tool}
              label={tool.replace('archigraph_', '')}
              active={filterTool === tool}
              onClick={() => setFilterTool((v) => v === tool ? null : tool)}
            />
          ))}

          {/* Divider */}
          {agentIds.length > 0 && (
            <span className="text-slate-300 dark:text-slate-700">|</span>
          )}

          {/* Agent chips */}
          {agentIds.map((agent) => (
            <FilterChip
              key={agent}
              label={agent}
              active={filterAgent === agent}
              color={agentColor(agent)}
              onClick={() => setFilterAgent((v) => v === agent ? null : agent)}
            />
          ))}

          {/* Clear all */}
          {(filterTool || filterAgent || searchQ) && (
            <button
              type="button"
              onClick={() => { setFilterTool(null); setFilterAgent(null); setSearchQ('') }}
              className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 flex items-center gap-1"
              data-testid="clear-filters-btn"
            >
              <X className="w-3 h-3" />
              Clear
            </button>
          )}
        </div>

        {/* ── Main content ───────────────────────────────────────────────── */}
        <div className="grid grid-cols-1 xl:grid-cols-[1fr_260px] gap-4 items-start">

          {/* Event list */}
          <div className="space-y-2" data-testid="activity-list">
            {isLoading && (
              <div className="space-y-2">
                {[...Array(5)].map((_, i) => (
                  <div key={i} className="h-16 rounded-lg bg-slate-100 dark:bg-slate-800 animate-pulse" />
                ))}
              </div>
            )}

            {!isLoading && filteredEvents.length === 0 && (
              <div className="flex flex-col items-center justify-center py-16 text-center">
                <Activity className="w-10 h-10 text-slate-300 dark:text-slate-600 mb-3" />
                <p className="text-sm font-medium text-slate-500 dark:text-slate-400">
                  {allEvents.length === 0
                    ? 'No MCP activity yet'
                    : 'No events match the current filters'}
                </p>
                <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
                  {allEvents.length === 0
                    ? 'Tool calls from MCP agents will appear here in real time.'
                    : 'Try clearing the search or filter chips above.'}
                </p>
                {(filterTool || filterAgent || searchQ) && (
                  <button
                    type="button"
                    onClick={() => { setFilterTool(null); setFilterAgent(null); setSearchQ('') }}
                    className="mt-3 text-xs text-sky-500 hover:text-sky-600 dark:text-sky-400"
                  >
                    Clear filters
                  </button>
                )}
              </div>
            )}

            {!isLoading && filteredEvents.map((event, idx) => (
              <EventRow
                key={`${event.timestamp}-${event.tool_name}-${idx}`}
                event={event}
                group={group}
                isNew={newIds.has(event.timestamp)}
                onReplay={handleReplay}
              />
            ))}

            <div ref={bottomRef} />
          </div>

          {/* Sidebar: tool stats */}
          <div className="space-y-4">
            <ToolStats events={allEvents} />

            {/* Navigation hint */}
            {allEvents.length > 0 && (
              <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
                <h2 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider mb-2">
                  Tip
                </h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 leading-relaxed">
                  Click any event row to open <strong>/graph/{group}</strong> with the
                  returned nodes pre-selected. Use "Replay on graph" in the expanded
                  detail to re-highlight at any time.
                </p>
                <button
                  type="button"
                  onClick={() => navigate(`/graph/${group}`)}
                  className="mt-3 text-xs text-sky-500 hover:text-sky-600 dark:text-sky-400 dark:hover:text-sky-300"
                >
                  Open graph →
                </button>
              </div>
            )}
          </div>
        </div>

        {/* Live tail timestamp footer */}
        {liveTail && (
          <div className="flex items-center gap-2 text-xs text-slate-400 pt-2">
            <Clock className="w-3.5 h-3.5" />
            <span>Live tail active — events appear as MCP agents query the graph</span>
          </div>
        )}
      </div>
    </div>
  )
}
