/**
 * System route — /system
 *
 * Daemon control panel: status, build info, restart/stop buttons, logs viewer.
 * Accessible from the top-nav "System" link (added to _layout.tsx) or directly
 * by navigating to /system.
 *
 * Implements OPERATIONS_PROMPT.md §1 (daemon control surface). Closes #1195.
 */

import { useState, useRef, useEffect, useCallback } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import {
  fetchSystemStatus,
  postSystemRestart,
  postSystemStop,
  fetchSystemLogs,
} from '@/api/client'
import type { SystemStatus, LogLine } from '@/types/api'
import {
  Activity, CheckCircle, XCircle, AlertTriangle,
  RotateCw, Power, ScrollText, Copy, RefreshCw,
  GitCommit, Clock, HardDrive, Server, Globe,
  ChevronDown, ChevronUp, Search, X,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function shortSHA(sha: string): string {
  if (!sha || sha === 'unknown') return 'unknown'
  return sha.length > 7 ? sha.slice(0, 7) : sha
}

function relativeTime(iso: string): string {
  if (!iso || iso === 'unknown') return 'unknown'
  const diff = Date.now() - new Date(iso).getTime()
  const secs = Math.floor(diff / 1000)
  if (secs < 60) return 'just now'
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) {
    const rem = mins % 60
    return rem > 0 ? `${hours}h ${rem}m ago` : `${hours}h ago`
  }
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function fmtMb(mb: number): string {
  return `${mb.toFixed(0)} MB`
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(() => {})
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badge
// ─────────────────────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: SystemStatus['status'] }) {
  const map = {
    running: { icon: <CheckCircle className="w-4 h-4" />, label: 'Running', cls: 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-950/40 border-emerald-200 dark:border-emerald-800' },
    stopped: { icon: <XCircle className="w-4 h-4" />, label: 'Stopped', cls: 'text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-950/40 border-red-200 dark:border-red-800' },
    unhealthy: { icon: <AlertTriangle className="w-4 h-4" />, label: 'Unhealthy', cls: 'text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40 border-amber-200 dark:border-amber-800' },
  }
  const { icon, label, cls } = map[status] ?? map.unhealthy
  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-sm font-medium ${cls}`}>
      {icon}
      {label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Confirm modal
// ─────────────────────────────────────────────────────────────────────────────

interface ConfirmModalProps {
  title: string
  body: string
  confirmLabel: string
  confirmClass: string
  onConfirm: () => void
  onCancel: () => void
}

function ConfirmModal({ title, body, confirmLabel, confirmClass, onConfirm, onCancel }: ConfirmModalProps) {
  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onCancel])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm" onClick={onCancel}>
      <div
        className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-2xl p-6 max-w-sm w-full mx-4"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
      >
        <h3 id="confirm-title" className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-2">{title}</h3>
        <p className="text-sm text-slate-600 dark:text-slate-400 mb-6">{body}</p>
        <div className="flex justify-end gap-3">
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 rounded-lg text-sm font-medium text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className={`px-4 py-2 rounded-lg text-sm font-medium text-white transition-colors ${confirmClass}`}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Logs panel
// ─────────────────────────────────────────────────────────────────────────────

const SEV_COLORS: Record<string, string> = {
  error: 'text-red-500 dark:text-red-400',
  warn:  'text-amber-500 dark:text-amber-400',
  info:  'text-slate-700 dark:text-slate-300',
  debug: 'text-slate-500 dark:text-slate-500',
}

interface LogsPanelProps {
  onClose: () => void
}

function LogsPanel({ onClose }: LogsPanelProps) {
  const [q, setQ] = useState('')
  const [severity, setSeverity] = useState<'error' | 'warn' | ''>('')
  const [autoScroll, setAutoScroll] = useState(true)
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['system-logs', q, severity],
    queryFn: () => fetchSystemLogs({ n: 200, q: q || undefined, severity: severity || undefined }),
    refetchInterval: autoScroll ? 3000 : false,
  })

  const lines: LogLine[] = data?.lines ?? []

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [lines, autoScroll])

  const copyAll = useCallback(() => {
    copyToClipboard(lines.map((l) => l.raw).join('\n'))
  }, [lines])

  return (
    <div
      className="fixed inset-y-0 right-0 z-50 w-full max-w-2xl bg-white dark:bg-slate-950 border-l border-slate-200 dark:border-slate-800 shadow-2xl flex flex-col"
      role="dialog"
      aria-label="Daemon logs"
      data-testid="logs-panel"
    >
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800 flex-shrink-0">
        <ScrollText className="w-4 h-4 text-slate-500" />
        <span className="font-semibold text-sm text-slate-900 dark:text-slate-100">Daemon logs</span>
        {data?.path && (
          <span className="text-xs text-slate-400 font-mono truncate">{data.path}</span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={copyAll}
            title="Copy all lines"
            className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <Copy className="w-3.5 h-3.5" />
          </button>
          <button
            type="button"
            onClick={() => refetch()}
            title="Refresh"
            className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close logs"
            className="p-1.5 rounded text-slate-500 hover:text-red-500 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 flex-shrink-0">
        <Search className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
        <input
          type="text"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filter logs…"
          className="flex-1 bg-transparent text-sm text-slate-900 dark:text-slate-100 placeholder-slate-400 outline-none"
          data-testid="logs-filter-input"
        />
        <select
          value={severity}
          onChange={(e) => setSeverity(e.target.value as 'error' | 'warn' | '')}
          className="text-xs bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 rounded px-2 py-1 border-0 outline-none cursor-pointer"
          aria-label="Filter by severity"
        >
          <option value="">All levels</option>
          <option value="warn">Warnings+</option>
          <option value="error">Errors only</option>
        </select>
        <label className="flex items-center gap-1.5 text-xs text-slate-500 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={autoScroll}
            onChange={(e) => setAutoScroll(e.target.checked)}
            className="rounded"
          />
          Auto-follow
        </label>
      </div>

      {/* Log lines */}
      <div className="flex-1 overflow-y-auto font-mono text-xs p-4 space-y-0.5" data-testid="logs-content">
        {isLoading && (
          <p className="text-slate-400 animate-pulse">Loading…</p>
        )}
        {!isLoading && lines.length === 0 && (
          <p className="text-slate-400">No log lines found. The daemon log may not exist yet.</p>
        )}
        {lines.map((line, i) => (
          <div key={i} className={`whitespace-pre-wrap break-all leading-5 ${SEV_COLORS[line.severity] ?? SEV_COLORS.info}`}>
            {line.raw}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main System route
// ─────────────────────────────────────────────────────────────────────────────

export function SystemRoute() {
  const [confirmAction, setConfirmAction] = useState<'restart' | 'stop' | null>(null)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [logsOpen, setLogsOpen] = useState(false)
  const [buildExpanded, setBuildExpanded] = useState(false)

  const { data: status, isLoading, error, refetch } = useQuery({
    queryKey: ['system-status'],
    queryFn: fetchSystemStatus,
    refetchInterval: 5000, // auto-refresh every 5s
  })

  const restartMutation = useMutation({
    mutationFn: postSystemRestart,
    onSuccess: (res: import('@/types/api').SystemActionReply) => {
      setActionMsg(res.message)
      setConfirmAction(null)
      // Re-poll aggressively after restart signal
      setTimeout(() => refetch(), 2000)
    },
  })

  const stopMutation = useMutation({
    mutationFn: postSystemStop,
    onSuccess: (res: import('@/types/api').SystemActionReply) => {
      setActionMsg(res.message)
      setConfirmAction(null)
    },
  })

  const commitSHA = status ? shortSHA(status.commit_sha) : null
  const commitUrl = commitSHA && commitSHA !== 'unknown'
    ? `https://github.com/cajasmota/archigraph/commit/${status!.commit_sha}`
    : null

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-2xl mx-auto px-6 py-8 space-y-6">

        {/* Page header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Server className="w-5 h-5 text-slate-500" />
            <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">System</h1>
          </div>
          <button
            type="button"
            onClick={() => refetch()}
            title="Refresh"
            className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
          </button>
        </div>

        {/* Action feedback banner */}
        {actionMsg && (
          <div className="flex items-start gap-2 px-4 py-3 rounded-lg bg-sky-50 dark:bg-sky-950/40 border border-sky-200 dark:border-sky-800 text-sky-700 dark:text-sky-300 text-sm">
            <Activity className="w-4 h-4 mt-0.5 flex-shrink-0" />
            <span className="flex-1">{actionMsg}</span>
            <button type="button" onClick={() => setActionMsg(null)}>
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        )}

        {/* Loading */}
        {isLoading && (
          <div className="animate-pulse space-y-4">
            <div className="h-24 rounded-xl bg-slate-100 dark:bg-slate-800" />
            <div className="h-16 rounded-xl bg-slate-100 dark:bg-slate-800" />
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="px-4 py-3 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 text-sm">
            Failed to load system status: {error instanceof Error ? error.message : String(error)}
          </div>
        )}

        {/* Status card */}
        {status && (
          <>
            <section className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden" data-testid="system-status-card">
              <div className="px-5 py-4 flex items-center justify-between border-b border-slate-100 dark:border-slate-800">
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Daemon</h2>
                <StatusBadge status={status.status} />
              </div>

              <div className="px-5 py-4 grid grid-cols-2 gap-4 text-sm">
                {/* PID */}
                <div>
                  <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">PID</div>
                  <div className="flex items-center gap-1.5 font-mono text-slate-900 dark:text-slate-100">
                    {status.pid}
                    <button
                      type="button"
                      onClick={() => copyToClipboard(String(status.pid))}
                      title="Copy PID"
                      className="p-0.5 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
                    >
                      <Copy className="w-3 h-3" />
                    </button>
                  </div>
                </div>

                {/* Uptime */}
                <div>
                  <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Uptime</div>
                  <div className="flex items-center gap-1.5 text-slate-900 dark:text-slate-100">
                    <Clock className="w-3.5 h-3.5 text-slate-400" />
                    {status.uptime_human ?? '—'}
                  </div>
                </div>

                {/* Memory */}
                <div>
                  <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Memory</div>
                  <div className="flex items-center gap-1.5 text-slate-900 dark:text-slate-100">
                    <HardDrive className="w-3.5 h-3.5 text-slate-400" />
                    {fmtMb(status.rss_mb)}
                    {status.rss_budget_mb ? (
                      <span className="text-slate-400">/ {fmtMb(status.rss_budget_mb)}</span>
                    ) : null}
                  </div>
                </div>

                {/* Socket */}
                {status.socket_path && (
                  <div>
                    <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Socket</div>
                    <div className="flex items-center gap-1.5 font-mono text-xs text-slate-700 dark:text-slate-300 truncate">
                      <Server className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                      <span className="truncate" title={status.socket_path}>{status.socket_path}</span>
                    </div>
                  </div>
                )}

                {/* Dashboard URL */}
                {status.dashboard_url && (
                  <div className="col-span-2">
                    <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Dashboard URL</div>
                    <div className="flex items-center gap-1.5 font-mono text-xs text-slate-700 dark:text-slate-300">
                      <Globe className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
                      {status.dashboard_url}
                    </div>
                  </div>
                )}
              </div>
            </section>

            {/* Build info card */}
            <section className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden" data-testid="system-build-card">
              <button
                type="button"
                onClick={() => setBuildExpanded((v) => !v)}
                className="w-full px-5 py-4 flex items-center justify-between text-left hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
                aria-expanded={buildExpanded}
              >
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Build info</h2>
                <div className="flex items-center gap-2">
                  {status.stale_build && (
                    <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-800">
                      Stale ({status.days_since_build}d ago)
                    </span>
                  )}
                  {buildExpanded ? <ChevronUp className="w-4 h-4 text-slate-400" /> : <ChevronDown className="w-4 h-4 text-slate-400" />}
                </div>
              </button>

              {buildExpanded && (
                <div className="px-5 pb-4 grid grid-cols-2 gap-4 text-sm border-t border-slate-100 dark:border-slate-800 pt-4">
                  {/* Version */}
                  <div>
                    <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Version</div>
                    <div className="font-mono text-slate-900 dark:text-slate-100">{status.version}</div>
                  </div>

                  {/* Commit */}
                  <div>
                    <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Commit</div>
                    <div className="flex items-center gap-1.5">
                      <GitCommit className="w-3.5 h-3.5 text-slate-400" />
                      {commitUrl ? (
                        <a
                          href={commitUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="font-mono text-sky-600 dark:text-sky-400 hover:text-sky-500 dark:hover:text-sky-300"
                          data-testid="commit-link"
                        >
                          {commitSHA}
                        </a>
                      ) : (
                        <span className="font-mono text-slate-700 dark:text-slate-300">{commitSHA}</span>
                      )}
                      <button
                        type="button"
                        onClick={() => copyToClipboard(status.commit_sha)}
                        title="Copy full SHA"
                        className="p-0.5 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
                      >
                        <Copy className="w-3 h-3" />
                      </button>
                    </div>
                  </div>

                  {/* Built at */}
                  <div className="col-span-2">
                    <div className="text-xs font-medium text-slate-500 dark:text-slate-500 mb-1">Built</div>
                    <div className="flex items-center gap-1.5 text-slate-700 dark:text-slate-300">
                      <Clock className="w-3.5 h-3.5 text-slate-400" />
                      <span title={status.built_at}>{relativeTime(status.built_at)}</span>
                      {status.days_since_build !== undefined && status.days_since_build > 0 && (
                        <span className="text-slate-400 text-xs">({status.days_since_build} days ago)</span>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </section>

            {/* Diagnostic report button */}
            <section className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden">
              <div className="px-5 py-4">
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Diagnostic report</h2>
                <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
                  Gather system status + last 500 log lines and copy to clipboard for support.
                </p>
                <DiagnosticReportButton status={status} />
              </div>
            </section>

            {/* Actions */}
            <section className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden">
              <div className="px-5 py-4">
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">Actions</h2>
                <div className="flex flex-wrap gap-3">
                  {/* View logs */}
                  <button
                    type="button"
                    onClick={() => setLogsOpen(true)}
                    className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
                    data-testid="view-logs-btn"
                  >
                    <ScrollText className="w-4 h-4" />
                    View logs
                  </button>

                  {/* Restart */}
                  <button
                    type="button"
                    onClick={() => setConfirmAction('restart')}
                    disabled={restartMutation.isPending}
                    className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 hover:bg-amber-200 dark:hover:bg-amber-900/70 border border-amber-200 dark:border-amber-800 transition-colors disabled:opacity-50"
                    data-testid="restart-btn"
                  >
                    <RotateCw className="w-4 h-4" />
                    Restart daemon
                  </button>

                  {/* Stop — danger zone */}
                  <button
                    type="button"
                    onClick={() => setConfirmAction('stop')}
                    disabled={stopMutation.isPending}
                    className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium bg-red-100 dark:bg-red-950/40 text-red-700 dark:text-red-400 hover:bg-red-200 dark:hover:bg-red-950/70 border border-red-200 dark:border-red-800 transition-colors disabled:opacity-50"
                    data-testid="stop-btn"
                  >
                    <Power className="w-4 h-4" />
                    Stop daemon
                  </button>
                </div>

                {/* Mutation errors */}
                {restartMutation.isError && (
                  <p className="mt-3 text-sm text-red-600 dark:text-red-400">
                    Restart failed: {restartMutation.error instanceof Error ? restartMutation.error.message : 'unknown error'}
                  </p>
                )}
                {stopMutation.isError && (
                  <p className="mt-3 text-sm text-red-600 dark:text-red-400">
                    Stop failed: {stopMutation.error instanceof Error ? stopMutation.error.message : 'unknown error'}
                  </p>
                )}
              </div>
            </section>
          </>
        )}
      </div>

      {/* Confirm modals */}
      {confirmAction === 'restart' && (
        <ConfirmModal
          title="Restart daemon?"
          body="The daemon will receive SIGTERM and restart automatically via launchd / systemd. Indexing in progress will be paused. The page will reconnect once the daemon is back."
          confirmLabel="Restart"
          confirmClass="bg-amber-500 hover:bg-amber-600"
          onConfirm={() => restartMutation.mutate()}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {confirmAction === 'stop' && (
        <ConfirmModal
          title="Stop daemon?"
          body="The daemon will stop and will NOT restart automatically. You will lose the dashboard until you manually run 'archigraph start' in a terminal."
          confirmLabel="Stop daemon"
          confirmClass="bg-red-600 hover:bg-red-700"
          onConfirm={() => stopMutation.mutate()}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {/* Logs slide-out panel */}
      {logsOpen && <LogsPanel onClose={() => setLogsOpen(false)} />}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// DiagnosticReportButton — gathers status + 500 log lines → clipboard
// ─────────────────────────────────────────────────────────────────────────────

function DiagnosticReportButton({ status }: { status: SystemStatus }) {
  const [copying, setCopying] = useState(false)

  const handleCopy = useCallback(async () => {
    setCopying(true)
    try {
      const logs = await fetchSystemLogs({ n: 500 })
      const report = [
        '=== Archigraph Diagnostic Report ===',
        `Generated: ${new Date().toISOString()}`,
        '',
        '--- System Status ---',
        `Status:   ${status.status}`,
        `PID:      ${status.pid}`,
        `Uptime:   ${status.uptime_human ?? '—'}`,
        `RSS:      ${status.rss_mb.toFixed(0)} MB / ${(status.rss_budget_mb ?? 500).toFixed(0)} MB`,
        `Version:  ${status.version} (${status.commit_sha})`,
        `Built:    ${status.built_at}`,
        `Socket:   ${status.socket_path ?? '—'}`,
        '',
        '--- Last 500 Log Lines ---',
        ...logs.lines.map((l) => l.raw),
      ].join('\n')
      await copyToClipboard(report)
    } finally {
      setCopying(false)
    }
  }, [status])

  return (
    <button
      type="button"
      onClick={handleCopy}
      disabled={copying}
      className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
      data-testid="diagnostic-report-btn"
    >
      <Copy className="w-4 h-4" />
      {copying ? 'Copying…' : 'Send diagnostic report'}
    </button>
  )
}
