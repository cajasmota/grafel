/**
 * Surface 7 — Patterns route (#1189)
 *
 * URL: /patterns/:group
 *
 * Shows agent-learned patterns with:
 *   - Stats header (total / pending review / rejected / stale)
 *   - Filterable list table
 *   - Detail side-panel (click a row)
 *   - Edit modal (JSON patch of mutable fields)
 *   - Delete with confirm
 *   - GC dry-run / execute flow
 *   - Export to CLAUDE.md (file path input)
 */

import { useState, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Sparkles,
  AlertTriangle,
  XCircle,
  Clock,
  CheckCircle,
  Trash2,
  Edit3,
  Download,
  RefreshCw,
  ChevronRight,
  X,
} from 'lucide-react'

import {
  fetchPatterns,
  fetchPatternDetail,
  updatePattern,
  deletePattern,
  runPatternGC,
  exportPatterns,
} from '@/api/client'
import type { PatternRow, PatternStats, PatternStatus } from '@/types/api'

const GROUP_DEFAULT = 'fixture-a'

// ─────────────────────────────────────────────────────────────────────────────
// Confidence badge
// ─────────────────────────────────────────────────────────────────────────────

function ConfidenceBadge({ value }: { value: number }) {
  const pct = Math.round(value * 100)
  const color =
    value >= 0.7
      ? 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300'
      : value >= 0.4
        ? 'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300'
        : 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300'
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-mono ${color}`}>
      {pct}%
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badge
// ─────────────────────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: PatternStatus }) {
  const map: Record<PatternStatus, string> = {
    active: 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300',
    candidate: 'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300',
    rejected: 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 line-through',
  }
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs ${map[status]}`}>
      {status}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Stats header
// ─────────────────────────────────────────────────────────────────────────────

function StatsHeader({ stats }: { stats: PatternStats }) {
  const items = [
    { label: 'Total', value: stats.total, icon: Sparkles, color: 'text-sky-500' },
    { label: 'Pending review', value: stats.pending_review, icon: Clock, color: 'text-amber-500' },
    { label: 'Rejected', value: stats.rejected, icon: XCircle, color: 'text-slate-400' },
    { label: 'Stale', value: stats.stale, icon: AlertTriangle, color: 'text-orange-500' },
  ]
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 p-4 border-b border-slate-200 dark:border-slate-800">
      {items.map(({ label, value, icon: Icon, color }) => (
        <div
          key={label}
          className="flex items-center gap-3 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900"
        >
          <Icon className={`w-4 h-4 flex-shrink-0 ${color}`} />
          <div>
            <div className="text-xl font-bold tabular-nums">{value}</div>
            <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
          </div>
        </div>
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Filter bar
// ─────────────────────────────────────────────────────────────────────────────

interface Filters {
  needsAttention: boolean
  status: PatternStatus | ''
  confidenceMin: number
}

function FilterBar({
  filters,
  onChange,
}: {
  filters: Filters
  onChange: (f: Filters) => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-3 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-slate-50/50 dark:bg-slate-900/30">
      {/* Needs attention toggle */}
      <label className="flex items-center gap-1.5 text-sm cursor-pointer select-none">
        <input
          type="checkbox"
          className="rounded"
          checked={filters.needsAttention}
          onChange={(e) => onChange({ ...filters, needsAttention: e.target.checked })}
        />
        <AlertTriangle className="w-3.5 h-3.5 text-amber-500" />
        Needs attention
      </label>

      {/* Status filter */}
      <select
        value={filters.status}
        onChange={(e) => onChange({ ...filters, status: e.target.value as PatternStatus | '' })}
        className="text-sm rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 px-2 py-1"
      >
        <option value="">All statuses</option>
        <option value="active">Active</option>
        <option value="candidate">Candidate</option>
        <option value="rejected">Rejected</option>
      </select>

      {/* Confidence range */}
      <label className="flex items-center gap-2 text-sm">
        <span className="text-slate-500">Confidence ≥</span>
        <input
          type="range"
          min={0}
          max={1}
          step={0.05}
          value={filters.confidenceMin}
          onChange={(e) =>
            onChange({ ...filters, confidenceMin: parseFloat(e.target.value) })
          }
          className="w-24"
        />
        <span className="font-mono text-xs w-8 text-right">
          {Math.round(filters.confidenceMin * 100)}%
        </span>
      </label>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Edit modal
// ─────────────────────────────────────────────────────────────────────────────

function EditModal({
  pattern,
  group,
  onClose,
}: {
  pattern: PatternRow
  group: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [fields, setFields] = useState({
    confidence: pattern.confidence,
    is_candidate: pattern.is_candidate,
    approval_note: pattern.approval_note ?? '',
    reject_reason: pattern.reject_reason ?? '',
  })

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => updatePattern(group, pattern.id, fields),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['patterns', group] })
      onClose()
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="w-full max-w-md bg-white dark:bg-slate-900 rounded-xl shadow-2xl border border-slate-200 dark:border-slate-700 p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-semibold text-sm">Edit pattern</h2>
          <button type="button" onClick={onClose}>
            <X className="w-4 h-4 text-slate-400" />
          </button>
        </div>

        <p className="text-xs text-slate-500 mb-4 font-mono break-all">{pattern.id}</p>

        <div className="space-y-3">
          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400">
            Confidence
            <input
              type="number"
              min={0}
              max={1}
              step={0.01}
              value={fields.confidence}
              onChange={(e) =>
                setFields((f) => ({ ...f, confidence: parseFloat(e.target.value) }))
              }
              className="mt-1 w-full rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2 py-1.5 text-sm"
            />
          </label>

          <label className="flex items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-400 cursor-pointer">
            <input
              type="checkbox"
              checked={fields.is_candidate}
              onChange={(e) => setFields((f) => ({ ...f, is_candidate: e.target.checked }))}
            />
            Is candidate (pending approval)
          </label>

          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400">
            Approval note
            <textarea
              rows={2}
              value={fields.approval_note}
              onChange={(e) => setFields((f) => ({ ...f, approval_note: e.target.value }))}
              className="mt-1 w-full rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2 py-1.5 text-sm"
            />
          </label>

          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400">
            Reject reason{' '}
            <span className="font-normal text-slate-400">(set to reject)</span>
            <textarea
              rows={2}
              value={fields.reject_reason}
              onChange={(e) => setFields((f) => ({ ...f, reject_reason: e.target.value }))}
              className="mt-1 w-full rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2 py-1.5 text-sm"
            />
          </label>
        </div>

        {error && (
          <p className="mt-3 text-xs text-red-600">{String(error)}</p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 rounded text-sm border border-slate-200 dark:border-slate-700"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => mutate()}
            disabled={isPending}
            className="px-3 py-1.5 rounded text-sm bg-sky-600 text-white hover:bg-sky-700 disabled:opacity-50"
          >
            {isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// GC modal
// ─────────────────────────────────────────────────────────────────────────────

function GCModal({ group, onClose }: { group: string; onClose: () => void }) {
  const qc = useQueryClient()
  const [phase, setPhase] = useState<'idle' | 'preview' | 'done'>('idle')
  const [preview, setPreview] = useState<{ pruned_count: number; candidate_decay_days: number } | null>(null)

  const { mutate: runDry, isPending: dryPending } = useMutation({
    mutationFn: () => runPatternGC(group, true),
    onSuccess: (data) => {
      setPreview({ pruned_count: data.pruned_count, candidate_decay_days: data.candidate_decay_days })
      setPhase('preview')
    },
  })

  const { mutate: runReal, isPending: realPending } = useMutation({
    mutationFn: () => runPatternGC(group, false),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['patterns', group] })
      setPhase('done')
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="w-full max-w-sm bg-white dark:bg-slate-900 rounded-xl shadow-2xl border border-slate-200 dark:border-slate-700 p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-semibold text-sm">Garbage collect patterns</h2>
          <button type="button" onClick={onClose}>
            <X className="w-4 h-4 text-slate-400" />
          </button>
        </div>

        {phase === 'idle' && (
          <>
            <p className="text-sm text-slate-600 dark:text-slate-400 mb-4">
              Prune candidate patterns that have not been validated recently. A dry-run preview will show what would be removed.
            </p>
            <button
              type="button"
              onClick={() => runDry()}
              disabled={dryPending}
              className="w-full py-2 rounded bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700 text-sm disabled:opacity-50"
            >
              {dryPending ? 'Analysing…' : 'Run dry-run preview'}
            </button>
          </>
        )}

        {phase === 'preview' && preview && (
          <>
            <p className="text-sm mb-3">
              <span className="font-semibold text-amber-600">{preview.pruned_count} candidate{preview.pruned_count !== 1 ? 's' : ''}</span>{' '}
              eligible for pruning (older than {preview.candidate_decay_days} days without validation).
            </p>
            {preview.pruned_count === 0 ? (
              <p className="text-sm text-emerald-600 flex items-center gap-1.5">
                <CheckCircle className="w-4 h-4" /> Nothing to prune.
              </p>
            ) : (
              <button
                type="button"
                onClick={() => runReal()}
                disabled={realPending}
                className="w-full py-2 rounded bg-red-600 text-white hover:bg-red-700 text-sm disabled:opacity-50"
              >
                {realPending ? 'Pruning…' : `Prune ${preview.pruned_count} pattern${preview.pruned_count !== 1 ? 's' : ''}`}
              </button>
            )}
          </>
        )}

        {phase === 'done' && (
          <p className="text-sm text-emerald-600 flex items-center gap-1.5">
            <CheckCircle className="w-4 h-4" /> Patterns pruned successfully.
          </p>
        )}

        <button type="button" onClick={onClose} className="mt-4 text-xs text-slate-400 hover:text-slate-600 w-full text-center">
          Close
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Export modal
// ─────────────────────────────────────────────────────────────────────────────

function ExportModal({ group, onClose }: { group: string; onClose: () => void }) {
  const [filePath, setFilePath] = useState('')
  const [result, setResult] = useState<string | null>(null)

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => exportPatterns(group, { file: filePath }),
    onSuccess: (data) => {
      setResult(`Exported ${data.exported} pattern${data.exported !== 1 ? 's' : ''} to ${data.target}`)
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="w-full max-w-sm bg-white dark:bg-slate-900 rounded-xl shadow-2xl border border-slate-200 dark:border-slate-700 p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-semibold text-sm">Export to CLAUDE.md</h2>
          <button type="button" onClick={onClose}>
            <X className="w-4 h-4 text-slate-400" />
          </button>
        </div>

        {!result ? (
          <>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">
              Absolute path to CLAUDE.md
            </label>
            <input
              type="text"
              placeholder="/path/to/repo/CLAUDE.md"
              value={filePath}
              onChange={(e) => setFilePath(e.target.value)}
              className="w-full rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2 py-1.5 text-sm mb-4"
            />
            {error && <p className="text-xs text-red-600 mb-3">{String(error)}</p>}
            <button
              type="button"
              onClick={() => mutate()}
              disabled={isPending || !filePath.trim()}
              className="w-full py-2 rounded bg-sky-600 text-white hover:bg-sky-700 text-sm disabled:opacity-50"
            >
              {isPending ? 'Exporting…' : 'Export'}
            </button>
          </>
        ) : (
          <p className="text-sm text-emerald-600 flex items-center gap-1.5">
            <CheckCircle className="w-4 h-4" /> {result}
          </p>
        )}

        <button type="button" onClick={onClose} className="mt-4 text-xs text-slate-400 hover:text-slate-600 w-full text-center">
          Close
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Detail panel
// ─────────────────────────────────────────────────────────────────────────────

function DetailPanel({
  pattern,
  group,
  onClose,
  onEdit,
  onDelete,
}: {
  pattern: PatternRow
  group: string
  onClose: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  // Pull full detail (includes all fields).
  const { data: detail } = useQuery({
    queryKey: ['pattern-detail', group, pattern.id],
    queryFn: () => fetchPatternDetail(group, pattern.id),
    // Fall back to list row if fetch fails.
    placeholderData: pattern,
  })

  const p = detail ?? pattern

  return (
    <aside className="flex flex-col h-full w-full max-w-md border-l border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-800 flex-shrink-0">
        <span className="font-semibold text-sm truncate">{p.trigger || p.id}</span>
        <button type="button" onClick={onClose} className="ml-2 flex-shrink-0">
          <X className="w-4 h-4 text-slate-400" />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4 text-sm">
        {/* Meta row */}
        <div className="flex flex-wrap gap-2">
          <StatusBadge status={p.status} />
          <ConfidenceBadge value={p.confidence} />
          {p.needs_attention && (
            <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300">
              <AlertTriangle className="w-3 h-3" /> Needs attention
            </span>
          )}
        </div>

        {/* ID */}
        <div>
          <p className="text-xs text-slate-400 mb-0.5">Pattern ID</p>
          <p className="font-mono text-xs break-all">{p.id}</p>
        </div>

        {/* Category */}
        {p.category && (
          <div>
            <p className="text-xs text-slate-400 mb-0.5">Category</p>
            <p className="text-xs">{p.category}</p>
          </div>
        )}

        {/* Observations */}
        <div>
          <p className="text-xs text-slate-400 mb-0.5">Observations</p>
          <p className="text-xs">{p.observations}</p>
        </div>

        {/* Last seen */}
        {p.last_seen && (
          <div>
            <p className="text-xs text-slate-400 mb-0.5">Last seen</p>
            <p className="text-xs">{new Date(p.last_seen).toLocaleString()}</p>
          </div>
        )}

        {/* Steps */}
        {p.steps && p.steps.length > 0 && (
          <div>
            <p className="text-xs text-slate-400 mb-1">Steps</p>
            <ol className="list-decimal list-inside space-y-1">
              {p.steps.map((step, i) => (
                <li key={i} className="text-xs text-slate-700 dark:text-slate-300">
                  {step}
                </li>
              ))}
            </ol>
          </div>
        )}

        {/* Anti-patterns */}
        {p.anti_patterns && p.anti_patterns.length > 0 && (
          <div>
            <p className="text-xs text-slate-400 mb-1">Anti-patterns</p>
            <ul className="space-y-1">
              {p.anti_patterns
                .filter((ap) => !ap.private)
                .map((ap, i) => (
                  <li key={i} className="text-xs text-slate-700 dark:text-slate-300">
                    <span className="text-red-500">✕</span> {ap.do_not}
                    {ap.reason && (
                      <span className="text-slate-400"> — {ap.reason}</span>
                    )}
                  </li>
                ))}
            </ul>
          </div>
        )}

        {/* Reject reason */}
        {p.reject_reason && (
          <div className="rounded bg-red-50 dark:bg-red-900/20 px-3 py-2">
            <p className="text-xs text-slate-400 mb-0.5">Reject reason</p>
            <p className="text-xs text-red-600 dark:text-red-400">{p.reject_reason}</p>
          </div>
        )}

        {/* Approval note */}
        {p.approval_note && (
          <div className="rounded bg-emerald-50 dark:bg-emerald-900/20 px-3 py-2">
            <p className="text-xs text-slate-400 mb-0.5">Approval note</p>
            <p className="text-xs text-emerald-700 dark:text-emerald-300">{p.approval_note}</p>
          </div>
        )}

        {/* Exemplars */}
        {p.exemplars && p.exemplars.length > 0 && (
          <div>
            <p className="text-xs text-slate-400 mb-1">Exemplar entity IDs</p>
            <ul className="space-y-0.5">
              {p.exemplars.map((id) => (
                <li key={id} className="font-mono text-xs text-slate-600 dark:text-slate-400">
                  {id}
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Full JSON */}
        <details className="mt-2">
          <summary className="text-xs text-slate-400 cursor-pointer select-none">
            Full JSON
          </summary>
          <pre className="mt-2 text-xs bg-slate-50 dark:bg-slate-900 p-3 rounded overflow-x-auto">
            {JSON.stringify(p, null, 2)}
          </pre>
        </details>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 px-4 py-3 border-t border-slate-200 dark:border-slate-800 flex-shrink-0">
        <button
          type="button"
          onClick={onEdit}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs border border-slate-200 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800"
        >
          <Edit3 className="w-3.5 h-3.5" /> Edit
        </button>
        <button
          type="button"
          onClick={onDelete}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs border border-red-200 dark:border-red-800 text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 ml-auto"
        >
          <Trash2 className="w-3.5 h-3.5" /> Delete
        </button>
      </div>
    </aside>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete confirm dialog
// ─────────────────────────────────────────────────────────────────────────────

function DeleteConfirm({
  pattern,
  group,
  onClose,
}: {
  pattern: PatternRow
  group: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const { mutate, isPending } = useMutation({
    mutationFn: () => deletePattern(group, pattern.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['patterns', group] })
      onClose()
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="w-full max-w-sm bg-white dark:bg-slate-900 rounded-xl shadow-2xl border border-slate-200 dark:border-slate-700 p-6">
        <h2 className="font-semibold text-sm mb-2">Delete pattern?</h2>
        <p className="text-sm text-slate-600 dark:text-slate-400 mb-4">
          This will permanently remove <span className="font-mono text-xs">{pattern.id}</span>. This action cannot be undone.
        </p>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 rounded text-sm border border-slate-200 dark:border-slate-700"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => mutate()}
            disabled={isPending}
            className="px-3 py-1.5 rounded text-sm bg-red-600 text-white hover:bg-red-700 disabled:opacity-50"
          >
            {isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Skeleton row
// ─────────────────────────────────────────────────────────────────────────────

function SkeletonRow() {
  return (
    <div className="animate-pulse flex items-center gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800">
      <div className="h-4 w-28 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-4 w-48 bg-slate-200 dark:bg-slate-700 rounded flex-1" />
      <div className="h-5 w-12 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-5 w-16 bg-slate-200 dark:bg-slate-700 rounded" />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main route
// ─────────────────────────────────────────────────────────────────────────────

export function PatternsRoute() {
  const { group = GROUP_DEFAULT } = useParams()

  const [filters, setFilters] = useState<Filters>({
    needsAttention: false,
    status: '',
    confidenceMin: 0,
  })

  const [selected, setSelected] = useState<PatternRow | null>(null)
  const [editing, setEditing] = useState<PatternRow | null>(null)
  const [deleting, setDeleting] = useState<PatternRow | null>(null)
  const [showGC, setShowGC] = useState(false)
  const [showExport, setShowExport] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: [
      'patterns',
      group,
      filters.needsAttention,
      filters.status,
      filters.confidenceMin,
    ],
    queryFn: () =>
      fetchPatterns(group, {
        needs_attention: filters.needsAttention || undefined,
        status: filters.status || undefined,
        confidence_min: filters.confidenceMin > 0 ? filters.confidenceMin : undefined,
      }),
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const handleSelect = useCallback((p: PatternRow) => {
    setSelected((prev) => (prev?.id === p.id ? null : p))
  }, [])

  const handleDelete = useCallback((p: PatternRow) => {
    setSelected(null)
    setDeleting(p)
  }, [])

  const patterns = data?.patterns ?? []
  const stats = data?.stats

  return (
    <div className="flex flex-col h-full">
      {/* Stats */}
      {stats && <StatsHeader stats={stats} />}

      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800">
        <span className="text-sm font-medium mr-auto">
          {isLoading ? 'Loading…' : `${patterns.length} pattern${patterns.length !== 1 ? 's' : ''}`}
        </span>
        <button
          type="button"
          onClick={() => setShowExport(true)}
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded text-xs border border-slate-200 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800"
        >
          <Download className="w-3.5 h-3.5" /> Export to CLAUDE.md
        </button>
        <button
          type="button"
          onClick={() => setShowGC(true)}
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded text-xs border border-slate-200 dark:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800"
        >
          <RefreshCw className="w-3.5 h-3.5" /> Run GC
        </button>
      </div>

      {/* Filters */}
      <FilterBar filters={filters} onChange={setFilters} />

      {/* Content */}
      <div className="flex flex-1 overflow-hidden">
        {/* List */}
        <div className="flex-1 overflow-y-auto">
          {error && (
            <div className="p-8 text-center text-sm text-red-600">
              Failed to load patterns: {String(error)}
            </div>
          )}

          {isLoading && (
            <>
              {Array.from({ length: 5 }).map((_, i) => (
                <SkeletonRow key={i} />
              ))}
            </>
          )}

          {!isLoading && !error && patterns.length === 0 && (
            <div className="flex flex-col items-center justify-center h-64 text-center gap-3">
              <Sparkles className="w-10 h-10 text-slate-300 dark:text-slate-700" />
              <p className="text-sm font-medium text-slate-500">No patterns found</p>
              <p className="text-xs text-slate-400 max-w-xs">
                {filters.needsAttention || filters.status || filters.confidenceMin > 0
                  ? 'Try removing filters.'
                  : 'Agents will populate this as they run on your code.'}
              </p>
            </div>
          )}

          {!isLoading &&
            patterns.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => handleSelect(p)}
                className={[
                  'w-full text-left flex items-center gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800 transition-colors',
                  selected?.id === p.id
                    ? 'bg-sky-50 dark:bg-sky-900/20'
                    : 'hover:bg-slate-50 dark:hover:bg-slate-900/50',
                ].join(' ')}
              >
                {/* Attention dot */}
                {p.needs_attention ? (
                  <AlertTriangle className="w-3.5 h-3.5 text-amber-500 flex-shrink-0" />
                ) : (
                  <CheckCircle className="w-3.5 h-3.5 text-slate-300 dark:text-slate-600 flex-shrink-0" />
                )}

                {/* Trigger / title */}
                <span className="flex-1 text-sm truncate min-w-0">
                  {p.trigger || p.id}
                </span>

                {/* Category */}
                {p.category && (
                  <span className="text-xs text-slate-400 hidden sm:block flex-shrink-0">
                    {p.category}
                  </span>
                )}

                {/* Confidence */}
                <ConfidenceBadge value={p.confidence} />

                {/* Status */}
                <StatusBadge status={p.status} />

                {/* Chevron */}
                <ChevronRight className="w-3.5 h-3.5 text-slate-300 flex-shrink-0" />
              </button>
            ))}
        </div>

        {/* Detail panel */}
        {selected && (
          <DetailPanel
            pattern={selected}
            group={group}
            onClose={() => setSelected(null)}
            onEdit={() => setEditing(selected)}
            onDelete={() => handleDelete(selected)}
          />
        )}
      </div>

      {/* Modals */}
      {editing && (
        <EditModal
          pattern={editing}
          group={group}
          onClose={() => setEditing(null)}
        />
      )}

      {deleting && (
        <DeleteConfirm
          pattern={deleting}
          group={group}
          onClose={() => setDeleting(null)}
        />
      )}

      {showGC && <GCModal group={group} onClose={() => setShowGC(false)} />}

      {showExport && (
        <ExportModal group={group} onClose={() => setShowExport(false)} />
      )}
    </div>
  )
}
