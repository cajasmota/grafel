/**
 * Audit Log — /audit-log
 *
 * Surface 14: Timeline of all state-changing operations (rebuild, reset,
 * settings update, pattern edit, enrichment trigger…).
 *
 * Backend:
 *   GET  /api/audit?limit=N&filter=op  — history from ~/.archigraph/audit.jsonl
 *   GET  /api/audit/stream             — SSE live tail
 *   GET  /api/audit/export?format=csv  — download
 *
 * Closes #1258.
 */

import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchAuditHistory,
  subscribeAuditStream,
} from '@/api/client'
import type { AuditEntry } from '@/types/api'
import {
  ClipboardList, Download, RefreshCw, X, Search,
  CheckCircle2, XCircle, Clock, ChevronDown, ChevronRight,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function fmtTimestamp(ts: string): string {
  try {
    return new Date(ts).toLocaleString(undefined, {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    })
  } catch {
    return ts
  }
}

function relTime(ts: string): string {
  try {
    const diff = Date.now() - new Date(ts).getTime()
    if (diff < 5_000) return 'just now'
    if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
    if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
    return `${Math.floor(diff / 86_400_000)}d ago`
  } catch {
    return ''
  }
}

/** Label for an operation — converts snake_case to Title Case words. */
function opLabel(op: string): string {
  return op.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

// ─────────────────────────────────────────────────────────────────────────────
// Filter chip
// ─────────────────────────────────────────────────────────────────────────────

interface ChipProps {
  label: string
  active: boolean
  onClick: () => void
}

function FilterChip({ label, active, onClick }: ChipProps) {
  const base = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-xs font-medium transition-colors cursor-pointer select-none'
  const cls = active
    ? `${base} bg-violet-100 dark:bg-violet-950/50 text-violet-700 dark:text-violet-300 border-violet-300 dark:border-violet-700`
    : `${base} bg-transparent text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-500`
  return (
    <button type="button" className={cls} onClick={onClick} aria-pressed={active}>
      {label}
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Stats sidebar
// ─────────────────────────────────────────────────────────────────────────────

function OpStats({ entries }: { entries: AuditEntry[] }) {
  const stats = useMemo(() => {
    const counts: Record<string, { total: number; errors: number }> = {}
    for (const e of entries) {
      if (!counts[e.operation]) counts[e.operation] = { total: 0, errors: 0 }
      counts[e.operation].total++
      if (e.result === 'error') counts[e.operation].errors++
    }
    return Object.entries(counts)
      .map(([op, v]) => ({ op, ...v }))
      .sort((a, b) => b.total - a.total)
      .slice(0, 8)
  }, [entries])

  if (stats.length === 0) return null
  const maxTotal = stats[0]?.total ?? 1

  return (
    <section
      className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4"
      data-testid="op-stats-panel"
    >
      <h2 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider mb-3">
        Operation counts
      </h2>
      <div className="space-y-2">
        {stats.map(({ op, total, errors }) => (
          <div key={op}>
            <div className="flex items-center justify-between text-xs mb-0.5">
              <span className="font-mono text-slate-700 dark:text-slate-300 truncate max-w-[60%]" title={op}>
                {opLabel(op)}
              </span>
              <span className="text-slate-400 tabular-nums">
                {total}{errors > 0 ? ` · ${errors} err` : ''}
              </span>
            </div>
            <div className="h-1 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
              <div
                className={[
                  'h-full rounded-full transition-all',
                  errors > 0
                    ? 'bg-rose-400 dark:bg-rose-500'
                    : 'bg-violet-400 dark:bg-violet-500',
                ].join(' ')}
                style={{ width: `${(total / maxTotal) * 100}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry row
// ─────────────────────────────────────────────────────────────────────────────

interface EntryRowProps {
  entry: AuditEntry
  isNew?: boolean
}

function EntryRow({ entry, isNew }: EntryRowProps) {
  const [expanded, setExpanded] = useState(false)
  const isError = entry.result === 'error'
  const hasParams = entry.params && Object.keys(entry.params).length > 0

  return (
    <div
      className={[
        'rounded-lg border transition-all',
        isNew
          ? 'border-violet-300 dark:border-violet-600 bg-violet-50/60 dark:bg-violet-950/30'
          : isError
          ? 'border-rose-200 dark:border-rose-800 bg-rose-50/40 dark:bg-rose-950/20'
          : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900',
      ].join(' ')}
      data-testid="audit-row"
    >
      {/* Main row */}
      <div className="flex items-start gap-3 px-4 py-3">
        {/* Status icon */}
        <div className="mt-0.5 flex-shrink-0">
          {isError
            ? <XCircle className="w-4 h-4 text-rose-500" aria-label="error" />
            : <CheckCircle2 className="w-4 h-4 text-emerald-500" aria-label="ok" />
          }
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="text-sm font-mono font-medium text-slate-900 dark:text-slate-100"
              data-testid="audit-op"
            >
              {entry.operation}
            </span>
            {entry.target && (
              <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400">
                {entry.target}
              </span>
            )}
            {isError && entry.error && (
              <span className="text-xs text-rose-600 dark:text-rose-400 truncate max-w-xs" title={entry.error}>
                {entry.error}
              </span>
            )}
          </div>
        </div>

        {/* Timestamp + expand */}
        <div className="flex items-center gap-2 flex-shrink-0">
          <time
            className="text-xs text-slate-400 tabular-nums"
            dateTime={entry.timestamp}
            title={fmtTimestamp(entry.timestamp)}
          >
            {relTime(entry.timestamp)}
          </time>
          {(hasParams || isError) && (
            <button
              type="button"
              aria-label={expanded ? 'Collapse details' : 'Expand details'}
              className="p-0.5 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
              onClick={() => setExpanded((v) => !v)}
            >
              {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
            </button>
          )}
        </div>
      </div>

      {/* Expanded detail */}
      {expanded && (
        <div
          className="px-4 pb-3 border-t border-slate-100 dark:border-slate-800 pt-3 space-y-2"
          data-testid="audit-row-detail"
        >
          <div className="flex items-center gap-4 text-xs text-slate-500 dark:text-slate-400">
            <Clock className="w-3.5 h-3.5" />
            <span>{fmtTimestamp(entry.timestamp)}</span>
          </div>
          {hasParams && (
            <div>
              <p className="text-xs font-semibold text-slate-500 dark:text-slate-400 mb-1">Parameters</p>
              <pre className="text-xs font-mono bg-slate-50 dark:bg-slate-950 rounded p-2 overflow-x-auto text-slate-700 dark:text-slate-300">
                {JSON.stringify(entry.params, null, 2)}
              </pre>
            </div>
          )}
          {isError && entry.error && (
            <div>
              <p className="text-xs font-semibold text-rose-500 dark:text-rose-400 mb-1">Error</p>
              <p className="text-xs font-mono text-rose-600 dark:text-rose-400">{entry.error}</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main route
// ─────────────────────────────────────────────────────────────────────────────

export function AuditLogRoute() {
  const [liveEntries, setLiveEntries] = useState<AuditEntry[]>([])
  const [newTimestamps, setNewTimestamps] = useState<Set<string>>(new Set())
  const [liveTail, setLiveTail] = useState(true)
  const [filterOp, setFilterOp] = useState<string | null>(null)
  const [filterResult, setFilterResult] = useState<'ok' | 'error' | null>(null)
  const [searchQ, setSearchQ] = useState('')
  const cleanupRef = useRef<(() => void) | null>(null)

  // ── History query ──────────────────────────────────────────────────────────
  const { data: historyData, isLoading, refetch } = useQuery({
    queryKey: ['audit-history'],
    queryFn: () => fetchAuditHistory(200),
    staleTime: 0,
  })

  const historyEntries: AuditEntry[] = historyData?.entries ?? []

  // ── Merge history + live (deduplicate by timestamp+operation) ─────────────
  const allEntries = useMemo(() => {
    const combined = [...historyEntries]
    for (const e of liveEntries) {
      const dup = combined.some(
        (h) => h.timestamp === e.timestamp && h.operation === e.operation,
      )
      if (!dup) combined.push(e)
    }
    // newest-first (timestamps are RFC3339 strings, lexicographic sort works)
    return combined.sort((a, b) => b.timestamp.localeCompare(a.timestamp))
  }, [historyEntries, liveEntries])

  // ── Live SSE subscription ──────────────────────────────────────────────────
  useEffect(() => {
    if (!liveTail) {
      cleanupRef.current?.()
      cleanupRef.current = null
      return
    }
    const cleanup = subscribeAuditStream(
      (entry) => {
        setLiveEntries((prev) => [entry, ...prev])
        setNewTimestamps((prev) => {
          const next = new Set(prev)
          next.add(entry.timestamp)
          setTimeout(
            () => setNewTimestamps((s) => { const n = new Set(s); n.delete(entry.timestamp); return n }),
            2_000,
          )
          return next
        })
      },
    )
    cleanupRef.current = cleanup
    return () => { cleanup(); cleanupRef.current = null }
  }, [liveTail])

  // ── Derived operation list for filter chips ────────────────────────────────
  const operations = useMemo(() => {
    const s = new Set(allEntries.map((e) => e.operation))
    return [...s].sort()
  }, [allEntries])

  // ── Filtered entries ───────────────────────────────────────────────────────
  const filteredEntries = useMemo(() => {
    let entries = allEntries
    if (filterOp) entries = entries.filter((e) => e.operation === filterOp)
    if (filterResult) entries = entries.filter((e) => e.result === filterResult)
    if (searchQ.trim()) {
      const q = searchQ.toLowerCase()
      entries = entries.filter((e) =>
        e.operation.includes(q) ||
        (e.target ?? '').toLowerCase().includes(q) ||
        (e.error ?? '').toLowerCase().includes(q),
      )
    }
    return entries
  }, [allEntries, filterOp, filterResult, searchQ])

  // ── Export ─────────────────────────────────────────────────────────────────
  const handleExportJSON = useCallback(() => {
    const blob = new Blob([JSON.stringify(filteredEntries, null, 2)], {
      type: 'application/json',
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `audit-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [filteredEntries])

  const handleExportCSV = useCallback(() => {
    window.open('/api/audit/export?format=csv', '_blank')
  }, [])

  const clearFilters = () => { setFilterOp(null); setFilterResult(null); setSearchQ('') }

  // ─────────────────────────────────────────────────────────────────────────
  return (
    <div className="h-full overflow-y-auto" data-testid="audit-log-page">
      <div className="max-w-5xl mx-auto px-4 sm:px-6 py-6 space-y-4">

        {/* ── Header ─────────────────────────────────────────────────────── */}
        <div className="flex items-center gap-3 flex-wrap">
          <ClipboardList className="w-5 h-5 text-slate-500 flex-shrink-0" />
          <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
            Audit Log
          </h1>
          <span className="text-xs text-slate-400 tabular-nums">
            {filteredEntries.length} event{filteredEntries.length !== 1 ? 's' : ''}
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
            <div className="relative group">
              <button
                type="button"
                title="Export"
                className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
                data-testid="export-btn"
                onClick={handleExportJSON}
              >
                <Download className="w-4 h-4" />
              </button>
              {/* CSV alt */}
              <button
                type="button"
                onClick={handleExportCSV}
                className="hidden group-hover:block absolute right-0 top-8 z-10 px-3 py-1.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg text-xs whitespace-nowrap text-slate-700 dark:text-slate-300"
              >
                Export as CSV
              </button>
            </div>
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
              placeholder="Search operation, target, error…"
              className="w-full pl-8 pr-3 py-1.5 rounded-lg bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 text-sm text-slate-900 dark:text-slate-100 placeholder-slate-400 outline-none focus:ring-2 focus:ring-violet-400/50"
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

          {/* Result filter */}
          <FilterChip
            label="Errors only"
            active={filterResult === 'error'}
            onClick={() => setFilterResult((v) => v === 'error' ? null : 'error')}
          />

          {operations.length > 0 && (
            <span className="text-slate-300 dark:text-slate-700">|</span>
          )}

          {/* Operation chips */}
          {operations.map((op) => (
            <FilterChip
              key={op}
              label={opLabel(op)}
              active={filterOp === op}
              onClick={() => setFilterOp((v) => v === op ? null : op)}
            />
          ))}

          {/* Clear all */}
          {(filterOp || filterResult || searchQ) && (
            <button
              type="button"
              onClick={clearFilters}
              className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 flex items-center gap-1"
              data-testid="clear-filters-btn"
            >
              <X className="w-3 h-3" />
              Clear
            </button>
          )}
        </div>

        {/* ── Main content ───────────────────────────────────────────────── */}
        <div className="grid grid-cols-1 xl:grid-cols-[1fr_240px] gap-4 items-start">

          {/* Entry list */}
          <div className="space-y-2" data-testid="audit-list">
            {isLoading && (
              <div className="space-y-2">
                {[...Array(5)].map((_, i) => (
                  <div key={i} className="h-12 rounded-lg bg-slate-100 dark:bg-slate-800 animate-pulse" />
                ))}
              </div>
            )}

            {!isLoading && filteredEntries.length === 0 && (
              <div className="flex flex-col items-center justify-center py-16 text-center">
                <ClipboardList className="w-10 h-10 text-slate-300 dark:text-slate-600 mb-3" />
                <p className="text-sm font-medium text-slate-500 dark:text-slate-400">
                  {allEntries.length === 0
                    ? 'No audit entries yet'
                    : 'No entries match the current filters'}
                </p>
                <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
                  {allEntries.length === 0
                    ? 'State-changing operations will appear here.'
                    : 'Try clearing the search or filter chips above.'}
                </p>
                {(filterOp || filterResult || searchQ) && (
                  <button
                    type="button"
                    onClick={clearFilters}
                    className="mt-3 text-xs text-violet-500 hover:text-violet-600 dark:text-violet-400"
                  >
                    Clear filters
                  </button>
                )}
              </div>
            )}

            {!isLoading && filteredEntries.map((entry, idx) => (
              <EntryRow
                key={`${entry.timestamp}-${entry.operation}-${idx}`}
                entry={entry}
                isNew={newTimestamps.has(entry.timestamp)}
              />
            ))}
          </div>

          {/* Sidebar: stats */}
          <div className="space-y-4">
            <OpStats entries={allEntries} />

            {/* Legend */}
            <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
              <h2 className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider mb-2">
                Logged operations
              </h2>
              <ul className="text-xs text-slate-500 dark:text-slate-400 space-y-1 leading-relaxed">
                {[
                  'rebuild / reset',
                  'cleanup',
                  'settings_update / reset',
                  'pattern_update / delete',
                  'enrichment_trigger',
                ].map((op) => (
                  <li key={op} className="font-mono">{op}</li>
                ))}
              </ul>
            </div>
          </div>
        </div>

        {/* Live tail footer */}
        {liveTail && (
          <div className="flex items-center gap-2 text-xs text-slate-400 pt-2">
            <Clock className="w-3.5 h-3.5" />
            <span>Live tail active — new operations appear automatically</span>
          </div>
        )}
      </div>
    </div>
  )
}
