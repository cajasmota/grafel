/**
 * IndexingProgressModal — full-screen overlay that streams indexing progress.
 *
 * Layout:
 *   ┌──────────────────────────────────────────────┐
 *   │  Title + helper text              [×]        │
 *   │  Overall progress bar  XX%                   │
 *   │  ─────────────────────────────────────────   │
 *   │  repo-a  [scanning]  12/100  ████░░░░ 12%   │
 *   │  repo-b  [done]      ───     ████████ 100%  │
 *   │  ...                                         │
 *   └──────────────────────────────────────────────┘
 *
 * Props:
 *   isOpen      — controls visibility.
 *   groupSlug   — slug of the group being indexed (drives useIndexProgress).
 *   onClose     — called when the user dismisses the modal manually.
 *
 * Auto-closes 2 s after status becomes 'done'.
 *
 * A minimised toaster is shown when the user navigates away while indexing
 * is still running (status === 'indexing').
 */

import { useEffect, useRef } from 'react'
import { X, Eye, Code2, Link2, Database, CheckCircle2, AlertCircle, Loader2 } from 'lucide-react'
import { useIndexProgress } from '@/hooks/shared/useIndexProgress'
import type { IndexPhase, RepoProgress } from '@/types/indexProgress'

// ──────────────────────────────────────────────────────────────────────────────
// Phase metadata
// ──────────────────────────────────────────────────────────────────────────────

interface PhaseInfo {
  label: string
  icon: React.ReactNode
  color: string
}

function phaseInfo(phase: IndexPhase): PhaseInfo {
  switch (phase) {
    case 'scanning':
      return { label: 'Scanning', icon: <Eye className="w-3 h-3" />, color: 'text-sky-400' }
    case 'extracting_ast':
      return { label: 'Extracting AST', icon: <Code2 className="w-3 h-3" />, color: 'text-violet-400' }
    case 'resolving_refs':
      return { label: 'Resolving refs', icon: <Link2 className="w-3 h-3" />, color: 'text-amber-400' }
    case 'writing_graph':
      return { label: 'Writing graph', icon: <Database className="w-3 h-3" />, color: 'text-emerald-400' }
    case 'done':
      return { label: 'Done', icon: <CheckCircle2 className="w-3 h-3" />, color: 'text-emerald-400' }
    case 'error':
      return { label: 'Error', icon: <AlertCircle className="w-3 h-3" />, color: 'text-red-400' }
    default:
      return { label: 'Waiting', icon: <Loader2 className="w-3 h-3 animate-spin" />, color: 'text-slate-400' }
  }
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

function fmtEta(ms?: number): string | null {
  if (!ms || ms <= 0) return null
  const secs = Math.ceil(ms / 1000)
  if (secs < 60) return `~${secs}s`
  const mins = Math.ceil(secs / 60)
  return `~${mins}m`
}

function fmtElapsed(startedAt?: string): string | null {
  if (!startedAt) return null
  const secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000)
  if (secs < 1) return null
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  const rem = secs % 60
  return rem > 0 ? `${mins}m ${rem}s` : `${mins}m`
}

function basename(path?: string): string {
  if (!path) return ''
  return path.split('/').pop() ?? path
}

// ──────────────────────────────────────────────────────────────────────────────
// ProgressBar
// ──────────────────────────────────────────────────────────────────────────────

interface ProgressBarProps {
  pct: number
  colorClass?: string
  'data-testid'?: string
}

function ProgressBar({ pct, colorClass = 'bg-sky-500', ...rest }: ProgressBarProps) {
  const clampedPct = Math.min(100, Math.max(0, pct))
  return (
    <div
      className="w-full h-2 rounded-full bg-slate-700 overflow-hidden"
      role="progressbar"
      aria-valuenow={clampedPct}
      aria-valuemin={0}
      aria-valuemax={100}
      {...rest}
    >
      <div
        className={`h-full rounded-full transition-all duration-500 ${colorClass}`}
        style={{ width: `${clampedPct}%` }}
      />
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────────────
// RepoRow
// ──────────────────────────────────────────────────────────────────────────────

interface RepoRowProps {
  repo: RepoProgress
}

function RepoRow({ repo }: RepoRowProps) {
  const info = phaseInfo(repo.phase)
  const pct =
    repo.files_total > 0
      ? Math.round((repo.files_done / repo.files_total) * 100)
      : repo.phase === 'done'
        ? 100
        : 0

  const eta = fmtEta(repo.eta_ms)
  const elapsed = fmtElapsed(repo.phase_started_at)
  const file = basename(repo.current_file)

  const barColor =
    repo.phase === 'done'
      ? 'bg-emerald-500'
      : repo.phase === 'error'
        ? 'bg-red-500'
        : 'bg-sky-500'

  return (
    <div
      className="flex flex-col gap-1.5 py-3 border-b border-slate-700/60 last:border-0"
      data-testid={`repo-row-${repo.slug}`}
    >
      {/* Header row: slug + phase badge + counters */}
      <div className="flex items-center gap-2 min-w-0">
        <span className="font-mono text-sm text-slate-200 truncate flex-1" title={repo.slug}>
          {repo.slug}
        </span>

        {/* Phase badge */}
        <span
          className={`flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full bg-slate-700 ${info.color} shrink-0`}
          data-testid={`repo-phase-${repo.slug}`}
        >
          {info.icon}
          {info.label}
        </span>

        {/* File counter */}
        {repo.files_total > 0 && (
          <span className="text-xs text-slate-400 font-mono shrink-0">
            {repo.files_done.toLocaleString()} / {repo.files_total.toLocaleString()}
          </span>
        )}

        {/* ETA or elapsed */}
        {eta ? (
          <span className="text-xs text-slate-500 shrink-0">{eta}</span>
        ) : elapsed ? (
          <span className="text-xs text-slate-500 shrink-0">{elapsed}</span>
        ) : null}
      </div>

      {/* Progress bar */}
      <ProgressBar pct={pct} colorClass={barColor} data-testid={`repo-bar-${repo.slug}`} />

      {/* Current file (extracting phase only) */}
      {file && repo.phase === 'extracting_ast' && (
        <p
          className="text-[10px] text-slate-500 font-mono truncate"
          title={repo.current_file}
          data-testid={`repo-file-${repo.slug}`}
        >
          {file}
        </p>
      )}

      {/* Error detail */}
      {repo.phase === 'error' && repo.error && (
        <p className="text-xs text-red-400 mt-0.5">{repo.error}</p>
      )}
    </div>
  )
}

// ──────────────────────────────────────────────────────────────────────────────
// MinimiseToaster — shown when modal is closed but indexing continues
// ──────────────────────────────────────────────────────────────────────────────

interface MinimiseToasterProps {
  pct: number
  groupSlug: string
  onReopen: () => void
}

function MinimiseToaster({ pct, groupSlug, onReopen }: MinimiseToasterProps) {
  return (
    <button
      type="button"
      onClick={onReopen}
      aria-label={`Indexing ${groupSlug} — ${pct}% complete. Click to view progress.`}
      data-testid="indexing-toaster"
      className={[
        'fixed bottom-4 right-4 z-50',
        'flex items-center gap-3 px-4 py-2.5 rounded-xl shadow-xl',
        'bg-slate-900 border border-slate-700 text-slate-200 text-sm',
        'hover:bg-slate-800 transition-colors cursor-pointer',
      ].join(' ')}
    >
      <Loader2 className="w-4 h-4 animate-spin text-sky-400 shrink-0" />
      <span className="font-mono text-xs truncate max-w-[8rem]">{groupSlug}</span>
      <span className="text-sky-400 font-semibold text-xs w-9 text-right">{pct}%</span>
      <div className="w-20 h-1.5 rounded-full bg-slate-700 overflow-hidden">
        <div
          className="h-full bg-sky-500 rounded-full transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </button>
  )
}

// ──────────────────────────────────────────────────────────────────────────────
// IndexingProgressModal (public)
// ──────────────────────────────────────────────────────────────────────────────

export interface IndexingProgressModalProps {
  isOpen: boolean
  groupSlug: string
  onClose: () => void
  /** Called when the user clicks the minimised toaster to restore the modal. Defaults to onClose if not provided. */
  onReopen?: () => void
}

const AUTO_CLOSE_DELAY_MS = 2000

export function IndexingProgressModal({
  isOpen,
  groupSlug,
  onClose,
  onReopen,
}: IndexingProgressModalProps) {
  const progress = useIndexProgress(groupSlug || undefined)
  const autoCloseRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Auto-close 2 s after reaching 'done' status.
  useEffect(() => {
    if (progress.status === 'done' && isOpen) {
      autoCloseRef.current = setTimeout(() => {
        progress.reset()
        onClose()
      }, AUTO_CLOSE_DELAY_MS)
    }
    return () => {
      if (autoCloseRef.current) clearTimeout(autoCloseRef.current)
    }
  }, [progress.status, isOpen, onClose, progress.reset]) // eslint-disable-line react-hooks/exhaustive-deps

  // Close on Escape.
  useEffect(() => {
    if (!isOpen) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [isOpen, onClose])

  // When modal is closed but indexing is still running, show the minimised toaster.
  const showToaster =
    !isOpen && (progress.status === 'indexing' || progress.status === 'connecting')

  if (!isOpen && !showToaster) return null

  if (showToaster) {
    return (
      <MinimiseToaster
        pct={progress.overall_pct}
        groupSlug={groupSlug}
        onReopen={onReopen ?? onClose}
      />
    )
  }

  const isDone = progress.status === 'done'
  const isError = progress.status === 'error'
  const isActive = progress.status === 'indexing' || progress.status === 'connecting'

  const overallBarColor = isDone
    ? 'bg-emerald-500'
    : isError
      ? 'bg-red-500'
      : 'bg-sky-500'

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm"
        aria-hidden
        onClick={onClose}
        data-testid="modal-backdrop"
      />

      {/* Dialog */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Indexing progress"
        data-testid="indexing-progress-modal"
        className={[
          'fixed inset-x-4 top-[10vh] z-50 mx-auto max-w-2xl',
          'flex flex-col rounded-2xl shadow-2xl',
          'bg-slate-900 border border-slate-700',
          'max-h-[80vh]',
        ].join(' ')}
      >
        {/* Header */}
        <div className="flex items-start justify-between px-6 pt-5 pb-4 border-b border-slate-700/60 shrink-0">
          <div className="flex-1 min-w-0 pr-4">
            <h2 className="text-lg font-semibold text-slate-100 flex items-center gap-2">
              {isDone ? (
                <CheckCircle2 className="w-5 h-5 text-emerald-400 shrink-0" />
              ) : isError ? (
                <AlertCircle className="w-5 h-5 text-red-400 shrink-0" />
              ) : (
                <Loader2 className="w-5 h-5 text-sky-400 animate-spin shrink-0" />
              )}
              {isDone
                ? 'Indexing complete'
                : isError
                  ? 'Indexing failed'
                  : 'Indexing your group…'}
            </h2>
            <p className="mt-1 text-sm text-slate-400">
              {isDone
                ? `${groupSlug} is ready to explore.`
                : isError
                  ? (progress.error ?? 'An error occurred. Check daemon logs for details.')
                  : '10–60 seconds per repo. You can leave this open and come back — progress streams from the daemon.'}
            </p>
          </div>

          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="p-1.5 rounded-lg text-slate-400 hover:text-slate-200 hover:bg-slate-800 transition-colors shrink-0"
            data-testid="modal-close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Overall progress */}
        <div className="px-6 py-4 border-b border-slate-700/60 shrink-0">
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">
              Overall
            </span>
            <span
              className="text-sm font-mono font-semibold text-slate-200"
              data-testid="overall-pct"
            >
              {progress.overall_pct}%
            </span>
          </div>
          <ProgressBar
            pct={progress.overall_pct}
            colorClass={overallBarColor}
            data-testid="overall-bar"
          />
        </div>

        {/* Per-repo list */}
        <div className="flex-1 overflow-y-auto px-6 py-2 min-h-0" data-testid="repo-list">
          {progress.repos.length === 0 && isActive && (
            <div className="flex items-center justify-center py-10 gap-2 text-slate-500">
              <Loader2 className="w-4 h-4 animate-spin" />
              <span className="text-sm">Connecting…</span>
            </div>
          )}
          {progress.repos.map((repo) => (
            <RepoRow key={repo.slug} repo={repo} />
          ))}
        </div>

        {/* Footer — auto-close notice when done */}
        {isDone && (
          <div
            className="px-6 py-3 border-t border-slate-700/60 shrink-0 text-center"
            data-testid="done-footer"
          >
            <p className="text-xs text-slate-500">
              This dialog will close automatically in 2 seconds.
            </p>
          </div>
        )}
      </div>
    </>
  )
}
