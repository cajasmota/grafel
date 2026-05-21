import { useState, useMemo, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchRepairs, fetchEnrichments, fetchCommunityNaming, postCandidateAction } from '@/api/client'
import type { PendingCandidateRow, QualificationSignal, EnrichmentProgressBand, CommunityNamingRow } from '@/types/api'
import { useEnrichmentProgress } from '@/hooks/shared/useEnrichmentProgress'
import {
  Wrench, Sparkles, CheckCircle, XCircle, AlertCircle,
  ChevronDown, ChevronRight, Search, EyeOff,
  ArrowUpDown, Loader2, Network,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Score derivation — stub for when #1131 backend hasn't shipped yet
// TODO: remove this derivation once #1131 is the minimum required backend version.
// ─────────────────────────────────────────────────────────────────────────────

const KIND_BASE_SCORES: Record<string, number> = {
  // high-value structural kinds
  http_endpoint:       80,
  Endpoint:            80,
  Route:               75,
  Service:             65,
  Controller:          65,
  View:                60,
  Schema:              60,
  Model:               60,
  DataAccess:          55,
  Process:             45,
  Operation:           40,
  Component:           35,
  // fallback
  default:             30,
}

function deriveScore(row: PendingCandidateRow): number {
  // If the backend already emits a score (from #1131), use it directly.
  if (row.score != null) return row.score

  const signals = row.qualification_signals ?? []
  const base = KIND_BASE_SCORES[row.entity_kind ?? row.kind] ?? KIND_BASE_SCORES.default

  let bonus = 0
  if (signals.includes('god_node'))          bonus += 20
  if (signals.includes('articulation_point')) bonus += 15
  if (signals.includes('high_pagerank'))      bonus += 10
  if (signals.includes('ambiguous_name'))     bonus += 15
  if (signals.includes('http_endpoint'))      bonus += 5

  // Fallback: use existing confidence field as a signal
  if (!signals.length && row.confidence != null) {
    bonus += Math.round(row.confidence * 20)
  }

  return Math.min(100, Math.max(0, base + bonus))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tier definitions
// ─────────────────────────────────────────────────────────────────────────────

export type TierId = 'critical' | 'high' | 'medium' | 'low'

interface TierDef {
  id: TierId
  label: string
  min: number
  max: number
  openByDefault: boolean
  color: string
  badgeColor: string
}

const TIERS: TierDef[] = [
  {
    id: 'critical',
    label: 'Critical',
    min: 80,
    max: 100,
    openByDefault: true,
    color: 'text-red-700 dark:text-red-400',
    badgeColor: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
  },
  {
    id: 'high',
    label: 'High',
    min: 60,
    max: 79,
    openByDefault: true,
    color: 'text-orange-700 dark:text-orange-400',
    badgeColor: 'bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 border-orange-200 dark:border-orange-700',
  },
  {
    id: 'medium',
    label: 'Medium',
    min: 40,
    max: 59,
    openByDefault: false,
    color: 'text-yellow-700 dark:text-yellow-400',
    badgeColor: 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
  },
  {
    id: 'low',
    label: 'Low',
    min: 0,
    max: 39,
    openByDefault: false,
    color: 'text-slate-500 dark:text-slate-400',
    badgeColor: 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700',
  },
]

function scoreTier(score: number): TierId {
  if (score >= 80) return 'critical'
  if (score >= 60) return 'high'
  if (score >= 40) return 'medium'
  return 'low'
}

// ─────────────────────────────────────────────────────────────────────────────
// ETA formatter
// ─────────────────────────────────────────────────────────────────────────────

function formatEta(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const m = Math.round(seconds / 60)
  return `~${m}m`
}

// ─────────────────────────────────────────────────────────────────────────────
// EnrichmentProgressPanel — live per-tier bars (#1286)
// ─────────────────────────────────────────────────────────────────────────────

const TIER_COLOR: Record<string, { bar: string; text: string; bg: string }> = {
  critical: {
    bar: 'bg-red-500 dark:bg-red-400',
    text: 'text-red-700 dark:text-red-400',
    bg: 'bg-red-100 dark:bg-red-900/20',
  },
  high: {
    bar: 'bg-orange-500 dark:bg-orange-400',
    text: 'text-orange-700 dark:text-orange-400',
    bg: 'bg-orange-100 dark:bg-orange-900/20',
  },
  medium: {
    bar: 'bg-yellow-500 dark:bg-yellow-400',
    text: 'text-yellow-700 dark:text-yellow-400',
    bg: 'bg-yellow-100 dark:bg-yellow-900/20',
  },
  low: {
    bar: 'bg-slate-400 dark:bg-slate-500',
    text: 'text-slate-600 dark:text-slate-400',
    bg: 'bg-slate-100 dark:bg-slate-800/40',
  },
}

interface TierProgressBarProps {
  band: EnrichmentProgressBand
}

function TierProgressBar({ band }: TierProgressBarProps) {
  const colors = TIER_COLOR[band.band] ?? TIER_COLOR.low
  const pct = band.total > 0 ? Math.round((band.done / band.total) * 100) : 0
  const hasWork = band.running > 0 || band.queued > 0
  const isNotStarted = band.total > 0 && band.done === 0 && !hasWork
  const isDone = band.total > 0 && band.done === band.total

  let statusText: string
  if (band.total === 0) {
    statusText = 'no jobs'
  } else if (isDone) {
    statusText = 'done'
  } else if (isNotStarted) {
    statusText = 'not started'
  } else if (band.eta_seconds != null) {
    statusText = `ETA ${formatEta(band.eta_seconds)}`
  } else if (hasWork) {
    statusText = 'calculating…'
  } else {
    statusText = `${band.done}/${band.total}`
  }

  const label = `${band.band.charAt(0).toUpperCase() + band.band.slice(1)}: ${band.done}/${band.total} done. ${statusText}`

  return (
    <div
      role="group"
      aria-label={`${band.band} tier enrichment progress`}
      className={`flex items-center gap-3 px-3 py-2 rounded-lg ${colors.bg}`}
    >
      {/* Tier label */}
      <span className={`w-16 text-xs font-semibold shrink-0 capitalize ${colors.text}`}>
        {band.band}
      </span>

      {/* Progress bar */}
      <div className="flex-1 min-w-0">
        <div
          role="progressbar"
          aria-label={label}
          aria-valuenow={pct}
          aria-valuemin={0}
          aria-valuemax={100}
          className="h-2 rounded-full bg-slate-200 dark:bg-slate-700 overflow-hidden"
        >
          <div
            className={`h-full rounded-full transition-all duration-700 ease-out ${colors.bar} ${band.running > 0 ? 'animate-pulse' : ''}`}
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      {/* Count */}
      <span className="w-20 text-xs tabular-nums text-slate-600 dark:text-slate-400 shrink-0 text-right">
        {band.total > 0 ? `${band.done}/${band.total}` : '—'}
      </span>

      {/* ETA / status */}
      <span className="w-24 text-xs text-slate-500 dark:text-slate-400 shrink-0 text-right">
        {statusText}
      </span>

      {/* Running spinner */}
      {band.running > 0 && (
        <Loader2 className="w-3.5 h-3.5 shrink-0 text-sky-500 animate-spin" aria-hidden="true" />
      )}
    </div>
  )
}

interface EnrichmentProgressPanelProps {
  group: string
}

/**
 * EnrichmentProgressPanel renders a collapsible live-progress overlay for
 * the enrichment queue (#1286). It auto-shows when jobs are active and
 * collapses once all tiers reach 100%.
 */
function EnrichmentProgressPanel({ group }: EnrichmentProgressPanelProps) {
  const { progress, isActive } = useEnrichmentProgress(group)
  const [collapsed, setCollapsed] = useState(false)

  // Don't render if we've never fetched or there are no jobs at all.
  if (progress == null || progress.overall_total === 0) return null

  const allDone = progress.overall_done === progress.overall_total
  // Once everything is done, collapse automatically after first render.
  if (allDone && !collapsed && !isActive) {
    // We don't mutate state here — we just show a "Completed" state.
  }

  return (
    <div
      data-testid="enrichment-progress-panel"
      className="mx-4 mt-3 mb-1 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 shadow-sm overflow-hidden"
    >
      {/* Header */}
      <button
        type="button"
        aria-expanded={!collapsed}
        aria-controls="enrichment-progress-body"
        onClick={() => setCollapsed((c) => !c)}
        className="w-full flex items-center gap-2 px-4 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
      >
        {collapsed
          ? <ChevronRight className="w-4 h-4 text-slate-400 shrink-0" />
          : <ChevronDown className="w-4 h-4 text-slate-400 shrink-0" />
        }
        <span className="text-sm font-semibold text-slate-700 dark:text-slate-300 flex items-center gap-2">
          Enrichment Progress
          {isActive && <Loader2 className="w-3.5 h-3.5 text-sky-500 animate-spin" aria-label="enrichment running" />}
        </span>
        <span className="ml-auto text-xs tabular-nums text-slate-500 dark:text-slate-400">
          {progress.overall_done}/{progress.overall_total}
          {allDone && <span className="ml-1.5 text-emerald-600 dark:text-emerald-400 font-medium">Complete</span>}
        </span>
      </button>

      {/* Body */}
      {!collapsed && (
        <div
          id="enrichment-progress-body"
          className="px-4 pb-3 space-y-1.5"
        >
          {progress.tiers.map((band) => (
            <TierProgressBar key={band.band} band={band} />
          ))}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Sort options
// ─────────────────────────────────────────────────────────────────────────────

type SortKey = 'score' | 'repo' | 'entity_kind' | 'discovered_at'

const SORT_OPTIONS: { id: SortKey; label: string }[] = [
  { id: 'score',        label: 'Score' },
  { id: 'repo',         label: 'Repo' },
  { id: 'entity_kind',  label: 'Kind' },
  { id: 'discovered_at', label: 'Newest' },
]

function sortRows(rows: PendingCandidateRow[], key: SortKey): PendingCandidateRow[] {
  return [...rows].sort((a, b) => {
    switch (key) {
      case 'score':        return deriveScore(b) - deriveScore(a)
      case 'repo':         return (a.repo ?? '').localeCompare(b.repo ?? '')
      case 'entity_kind':  return (a.entity_kind ?? a.kind ?? '').localeCompare(b.entity_kind ?? b.kind ?? '')
      case 'discovered_at':
        return ((b.discovered_at ?? '') > (a.discovered_at ?? '') ? 1 : -1)
      default: return 0
    }
  })
}

// ─────────────────────────────────────────────────────────────────────────────
// LocalStorage helpers
// ─────────────────────────────────────────────────────────────────────────────

const LS_PREFIX = 'archigraph.pending'

function lsGet<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(`${LS_PREFIX}.${key}`)
    return raw != null ? (JSON.parse(raw) as T) : fallback
  } catch { return fallback }
}

function lsSet(key: string, value: unknown) {
  try { localStorage.setItem(`${LS_PREFIX}.${key}`, JSON.stringify(value)) } catch { /* noop */ }
}

// ─────────────────────────────────────────────────────────────────────────────
// Skeleton row
// ─────────────────────────────────────────────────────────────────────────────

function SkeletonRow() {
  return (
    <div className="animate-pulse flex items-center gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800">
      <div className="h-4 w-14 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-4 w-24 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-4 w-40 bg-slate-200 dark:bg-slate-700 rounded flex-1" />
      <div className="h-4 w-16 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-6 w-14 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-6 w-14 bg-slate-200 dark:bg-slate-700 rounded" />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Qualification signal chips
// ─────────────────────────────────────────────────────────────────────────────

const SIGNAL_LABELS: Record<QualificationSignal, string> = {
  http_endpoint:      'HTTP',
  god_node:           'God node',
  articulation_point: 'Articulation pt',
  high_pagerank:      'High PageRank',
  ambiguous_name:     'Ambiguous name',
  schema_model:       'Schema/Model',
  data_access:        'DataAccess',
  service_controller: 'Service/Ctrl',
}

function SignalChips({ signals }: { signals: QualificationSignal[] }) {
  if (!signals.length) return null
  return (
    <div className="flex flex-wrap gap-1 mt-1">
      {signals.map((s) => (
        <span
          key={s}
          className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-purple-50 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-700"
        >
          {SIGNAL_LABELS[s] ?? s}
        </span>
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function subjectLabel(row: PendingCandidateRow): string {
  const ctx = row.context ?? {}
  if (typeof ctx.original_stub === 'string' && ctx.original_stub) return ctx.original_stub
  if (typeof ctx.path === 'string' && ctx.path) return ctx.path
  if (typeof ctx.name === 'string' && ctx.name) return ctx.name
  if (typeof ctx.auto_name === 'string' && ctx.auto_name) return ctx.auto_name
  return row.subject_id
}

function contextSummary(row: PendingCandidateRow): string {
  const ctx = row.context ?? {}
  const parts: string[] = []
  if (typeof ctx.relation === 'string') parts.push(ctx.relation)
  if (typeof ctx.disposition === 'string') parts.push(ctx.disposition)
  if (typeof ctx.disposition_reason === 'string') parts.push(ctx.disposition_reason)
  if (typeof ctx.category === 'string') parts.push(ctx.category)
  if (typeof ctx.dynamic_kind === 'string') parts.push(ctx.dynamic_kind)
  if (typeof ctx.kind === 'string' && !parts.length) parts.push(ctx.kind)
  return parts.slice(0, 2).join(' · ') || '—'
}

function proposedValue(row: PendingCandidateRow): string | null {
  const ctx = row.context ?? {}
  if (typeof ctx.proposed_value === 'string' && ctx.proposed_value) return ctx.proposed_value
  if (typeof ctx.static_path_suffix === 'string' && ctx.static_path_suffix) return ctx.static_path_suffix
  return null
}

// ─────────────────────────────────────────────────────────────────────────────
// CandidateRow
// ─────────────────────────────────────────────────────────────────────────────

interface CandidateRowProps {
  row: PendingCandidateRow
  group: string
  tier: TierDef
  selected: boolean
  onToggleSelect: (id: string) => void
  onApplied: () => void
}

function CandidateRow({ row, group, tier, selected, onToggleSelect, onApplied }: CandidateRowProps) {
  const qc = useQueryClient()
  const [actionResult, setActionResult] = useState<'applied' | 'rejected' | null>(null)

  const mutation = useMutation({
    mutationFn: (action: 'apply' | 'reject') =>
      postCandidateAction(group, row.candidate_id, action),
    onSuccess: (_, action) => {
      setActionResult(action === 'apply' ? 'applied' : 'rejected')
      void qc.invalidateQueries({ queryKey: ['repairs', group] })
      void qc.invalidateQueries({ queryKey: ['enrichments', group] })
      onApplied()
    },
  })

  const subject = subjectLabel(row)
  const summary = contextSummary(row)
  const proposed = proposedValue(row)
  const score = deriveScore(row)
  const signals = row.qualification_signals ?? []

  if (actionResult) {
    return (
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-100 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-500 italic">
        {actionResult === 'applied'
          ? <CheckCircle className="w-3.5 h-3.5 text-emerald-500" />
          : <XCircle className="w-3.5 h-3.5 text-slate-400" />
        }
        {actionResult === 'applied' ? 'Applied' : 'Rejected'}: {subject}
      </div>
    )
  }

  return (
    <div
      className={[
        'flex items-start gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800',
        'hover:bg-slate-50/60 dark:hover:bg-slate-800/40 transition-colors group',
        selected ? 'bg-sky-50/50 dark:bg-sky-900/10' : '',
      ].join(' ')}
    >
      {/* Checkbox */}
      <input
        type="checkbox"
        aria-label={`Select candidate ${row.candidate_id}`}
        checked={selected}
        onChange={() => onToggleSelect(row.candidate_id)}
        className="mt-1 h-3.5 w-3.5 shrink-0 rounded border-slate-300 dark:border-slate-600 text-sky-600 focus:ring-sky-500"
      />

      {/* Score badge */}
      <span
        className={`shrink-0 mt-0.5 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-bold border ${tier.badgeColor}`}
        title={`Score: ${score}`}
      >
        {score}
      </span>

      {/* Kind badge */}
      <span className="shrink-0 mt-0.5 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border border-sky-200 dark:border-sky-700">
        {row.entity_kind ?? row.kind}
      </span>

      {/* Subject + context */}
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-slate-800 dark:text-slate-200 truncate" title={subject}>
          {subject}
        </p>
        <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 truncate">
          {summary}
          {row.repo && (
            <span className="ml-2 font-mono text-slate-400 dark:text-slate-500">{row.repo}</span>
          )}
        </p>
        {proposed && (
          <p className="text-xs text-emerald-700 dark:text-emerald-400 mt-0.5 truncate font-mono" title={proposed}>
            Proposed: {proposed}
          </p>
        )}
        <SignalChips signals={signals as QualificationSignal[]} />
      </div>


      {/* Actions */}
      <div className="shrink-0 flex items-center gap-1.5 opacity-0 group-hover:opacity-100 transition-opacity focus-within:opacity-100">
        <button
          type="button"
          aria-label={`Apply candidate ${row.candidate_id}`}
          disabled={mutation.isPending}
          onClick={() => mutation.mutate('apply')}
          className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium rounded border border-emerald-300 dark:border-emerald-700 text-emerald-700 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-900/30 hover:bg-emerald-100 dark:hover:bg-emerald-900/60 disabled:opacity-50 transition-colors"
        >
          <CheckCircle className="w-3 h-3" />
          Apply
        </button>
        <button
          type="button"
          aria-label={`Reject candidate ${row.candidate_id}`}
          disabled={mutation.isPending}
          onClick={() => mutation.mutate('reject')}
          className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium rounded border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 bg-white dark:bg-slate-900 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-50 transition-colors"
        >
          <XCircle className="w-3 h-3" />
          Reject
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// TierSection — collapsible bucket with pagination + bulk select + hide-forever
// ─────────────────────────────────────────────────────────────────────────────

const PAGE_SIZE = 25

interface TierSectionProps {
  tier: TierDef
  rows: PendingCandidateRow[]
  group: string
  sortKey: SortKey
  expanded: boolean
  onToggleExpanded: () => void
  selectedIds: Set<string>
  onToggleSelect: (id: string) => void
  onSelectAll: (ids: string[]) => void
  onClearAll: (ids: string[]) => void
  onApplied: () => void
  onHideForever: (tierId: TierId) => void
}

function TierSection({
  tier, rows, group, sortKey,
  expanded, onToggleExpanded,
  selectedIds, onToggleSelect, onSelectAll, onClearAll,
  onApplied, onHideForever,
}: TierSectionProps) {
  const [page, setPage] = useState(1)

  const sorted = useMemo(() => sortRows(rows, sortKey), [rows, sortKey])
  const pageRows = sorted.slice(0, page * PAGE_SIZE)
  const hasMore = sorted.length > pageRows.length

  const tierIds = sorted.map((r) => r.candidate_id)
  const selectedInTier = tierIds.filter((id) => selectedIds.has(id))
  const allSelected = tierIds.length > 0 && tierIds.every((id) => selectedIds.has(id))

  const headerBg =
    tier.id === 'critical' ? 'bg-red-50/80 dark:bg-red-900/10 border-red-200 dark:border-red-800/50' :
    tier.id === 'high'     ? 'bg-orange-50/80 dark:bg-orange-900/10 border-orange-200 dark:border-orange-800/50' :
    tier.id === 'medium'   ? 'bg-yellow-50/80 dark:bg-yellow-900/10 border-yellow-200 dark:border-yellow-800/50' :
                             'bg-slate-50/80 dark:bg-slate-900/30 border-slate-200 dark:border-slate-800'

  return (
    <section
      aria-label={`${tier.label} tier`}
      data-tier={tier.id}
      className="border border-slate-200 dark:border-slate-800 rounded-lg overflow-hidden mb-3"
    >
      {/* ── Tier header ───────────────────────────────────────────────────── */}
      <div className={`flex items-center gap-2 px-4 py-2.5 border-b ${headerBg}`}>
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={`tier-body-${tier.id}`}
          onClick={onToggleExpanded}
          className="flex items-center gap-2 flex-1 min-w-0 text-left"
        >
          {expanded
            ? <ChevronDown className={`w-4 h-4 shrink-0 ${tier.color}`} />
            : <ChevronRight className={`w-4 h-4 shrink-0 ${tier.color}`} />
          }
          <span className={`text-sm font-semibold ${tier.color}`}>{tier.label}</span>
          <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-bold border ${tier.badgeColor}`}>
            {rows.length}
          </span>
        </button>

        {/* Bulk select all in tier */}
        {expanded && rows.length > 0 && (
          <button
            type="button"
            onClick={() => allSelected ? onClearAll(tierIds) : onSelectAll(tierIds)}
            className="text-xs text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 transition-colors shrink-0"
          >
            {allSelected ? 'Deselect all' : `Select all ${rows.length}`}
          </button>
        )}

        {/* Selected count in tier */}
        {selectedInTier.length > 0 && (
          <span className="text-xs text-sky-600 dark:text-sky-400 tabular-nums shrink-0">
            {selectedInTier.length} selected
          </span>
        )}

        {/* Hide forever stub */}
        <button
          type="button"
          aria-label={`Hide ${tier.label} tier forever`}
          onClick={() => onHideForever(tier.id)}
          title="Flag all as enrichment_not_wanted (stub)"
          className="shrink-0 inline-flex items-center gap-1 px-2 py-1 text-xs rounded border border-slate-200 dark:border-slate-700 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 hover:border-slate-300 dark:hover:border-slate-600 transition-colors"
        >
          <EyeOff className="w-3 h-3" />
          Hide forever
        </button>
      </div>

      {/* ── Tier body ─────────────────────────────────────────────────────── */}
      {expanded && (
        <div id={`tier-body-${tier.id}`}>
          {rows.length === 0 ? (
            <div className="flex items-center justify-center py-8 text-xs text-slate-400 dark:text-slate-500">
              No candidates in this tier
            </div>
          ) : (
            <>
              {pageRows.map((row) => (
                <CandidateRow
                  key={row.candidate_id}
                  row={row}
                  group={group}
                  tier={tier}
                  selected={selectedIds.has(row.candidate_id)}
                  onToggleSelect={onToggleSelect}
                  onApplied={onApplied}
                />
              ))}
              {hasMore && (
                <div className="flex justify-center py-3 border-t border-slate-100 dark:border-slate-800">
                  <button
                    type="button"
                    onClick={() => setPage((p) => p + 1)}
                    className="text-xs text-sky-600 dark:text-sky-400 hover:underline"
                  >
                    Show more ({sorted.length - pageRows.length} remaining)
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </section>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// TieredList — bucketing + tier filter chips + search + sort
// ─────────────────────────────────────────────────────────────────────────────

interface TieredListProps {
  rows: PendingCandidateRow[]
  group: string
  emptyMessage: string
  onApplied: () => void
}

function TieredList({ rows, group, emptyMessage, onApplied }: TieredListProps) {
  // Tier expanded state — persisted to localStorage
  const [expandedTiers, setExpandedTiers] = useState<Record<TierId, boolean>>(() =>
    lsGet('tierExpanded', Object.fromEntries(TIERS.map((t) => [t.id, t.openByDefault])) as Record<TierId, boolean>)
  )

  // Tier visibility (filter chips)
  const [visibleTiers, setVisibleTiers] = useState<Set<TierId>>(
    () => new Set(lsGet<TierId[]>('visibleTiers', TIERS.map((t) => t.id)))
  )

  // Search
  const [searchQ, setSearchQ] = useState('')

  // Sort
  const [sortKey, setSortKey] = useState<SortKey>('score')

  // Bulk select
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  // Hide-forever stubs (flag only — no backend action yet)
  const [hiddenTiers, setHiddenTiers] = useState<Set<TierId>>(new Set())

  const toggleExpanded = useCallback((tierId: TierId) => {
    setExpandedTiers((prev) => {
      const next = { ...prev, [tierId]: !prev[tierId] }
      lsSet('tierExpanded', next)
      return next
    })
  }, [])

  const toggleVisibleTier = useCallback((tierId: TierId) => {
    setVisibleTiers((prev) => {
      const next = new Set(prev)
      if (next.has(tierId)) { next.delete(tierId) } else { next.add(tierId) }
      lsSet('visibleTiers', Array.from(next))
      return next
    })
  }, [])

  const handleSelectAll = useCallback((ids: string[]) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      ids.forEach((id) => next.add(id))
      return next
    })
  }, [])

  const handleClearAll = useCallback((ids: string[]) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      ids.forEach((id) => next.delete(id))
      return next
    })
  }, [])

  const handleToggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) { next.delete(id) } else { next.add(id) }
      return next
    })
  }, [])

  const handleHideForever = useCallback((tierId: TierId) => {
    // Stub — in a future sprint this POSTs enrichment_not_wanted for each candidate in the tier.
    // For now, just hide the tier from the current session.
    console.info(`[pending] hide-forever stub for tier: ${tierId} — enrichment_not_wanted not yet wired`)
    setHiddenTiers((prev) => new Set([...prev, tierId]))
  }, [])

  // Filter + search rows
  const q = searchQ.trim().toLowerCase()
  const bucketed = useMemo(() => {
    const filtered = q
      ? rows.filter((r) => {
          const label = subjectLabel(r).toLowerCase()
          const repo  = (r.repo ?? '').toLowerCase()
          const kind  = (r.entity_kind ?? r.kind ?? '').toLowerCase()
          return label.includes(q) || repo.includes(q) || kind.includes(q)
        })
      : rows

    return TIERS.map((tier) => ({
      tier,
      rows: filtered.filter((r) => scoreTier(deriveScore(r)) === tier.id),
    }))
  }, [rows, q])

  const totalSelected = selectedIds.size

  return (
    <div className="flex flex-col h-full">
      {/* ── Toolbar ───────────────────────────────────────────────────────── */}
      <div className="flex items-center gap-2 flex-wrap px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 backdrop-blur-sm flex-shrink-0">

        {/* Search */}
        <div className="flex items-center gap-1.5 flex-1 min-w-[160px] max-w-xs bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-2.5 py-1.5">
          <Search className="w-3.5 h-3.5 text-slate-400 dark:text-slate-500 shrink-0" />
          <input
            type="search"
            aria-label="Search candidates"
            placeholder="Search…"
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            className="flex-1 bg-transparent text-sm text-slate-700 dark:text-slate-300 placeholder-slate-400 dark:placeholder-slate-500 outline-none"
          />
        </div>

        {/* Tier filter chips */}
        <div className="flex items-center gap-1" role="group" aria-label="Filter by tier">
          {TIERS.map((tier) => {
            const active = visibleTiers.has(tier.id)
            return (
              <button
                key={tier.id}
                type="button"
                aria-pressed={active}
                onClick={() => toggleVisibleTier(tier.id)}
                className={[
                  'px-2.5 py-1 rounded-full text-xs font-semibold border transition-colors',
                  active
                    ? `${tier.badgeColor} opacity-100`
                    : 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500 border-slate-200 dark:border-slate-700 opacity-60',
                ].join(' ')}
              >
                {tier.label}
              </button>
            )
          })}
        </div>

        {/* Sort */}
        <div className="flex items-center gap-1.5 ml-auto">
          <ArrowUpDown className="w-3.5 h-3.5 text-slate-400" />
          <select
            aria-label="Sort by"
            value={sortKey}
            onChange={(e) => setSortKey(e.target.value as SortKey)}
            className="text-xs bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 text-slate-700 dark:text-slate-300 outline-none focus:ring-1 focus:ring-sky-500"
          >
            {SORT_OPTIONS.map((o) => (
              <option key={o.id} value={o.id}>{o.label}</option>
            ))}
          </select>
        </div>

        {/* Bulk selection feedback */}
        {totalSelected > 0 && (
          <span className="text-xs text-sky-600 dark:text-sky-400 font-medium tabular-nums">
            {totalSelected} selected
          </span>
        )}
      </div>

      {/* ── Tier sections ─────────────────────────────────────────────────── */}
      <div className="flex-1 overflow-y-auto p-4 space-y-0">
        {bucketed.map(({ tier, rows: tierRows }) => {
          if (!visibleTiers.has(tier.id)) return null
          if (hiddenTiers.has(tier.id)) return null
          return (
            <TierSection
              key={tier.id}
              tier={tier}
              rows={tierRows}
              group={group}
              sortKey={sortKey}
              expanded={expandedTiers[tier.id] ?? tier.openByDefault}
              onToggleExpanded={() => toggleExpanded(tier.id)}
              selectedIds={selectedIds}
              onToggleSelect={handleToggleSelect}
              onSelectAll={handleSelectAll}
              onClearAll={handleClearAll}
              onApplied={onApplied}
              onHideForever={handleHideForever}
            />
          )
        })}

        {/* Search: no results match */}
        {q && bucketed.every(({ tier, rows: r }) =>
          !visibleTiers.has(tier.id) || hiddenTiers.has(tier.id) || r.length === 0
        ) && (
          <div className="flex flex-col items-center justify-center py-16 gap-2 text-slate-400 dark:text-slate-500">
            <Search className="w-8 h-8 text-slate-300" />
            <p className="text-sm">No candidates match "{searchQ}"</p>
          </div>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// CommunityNamingList — flat list of name_community candidates (#1301)
// ─────────────────────────────────────────────────────────────────────────────

interface CommunityNamingListProps {
  rows: CommunityNamingRow[]
}

function CommunityNamingList({ rows }: CommunityNamingListProps) {
  const [searchQ, setSearchQ] = useState('')
  const q = searchQ.trim().toLowerCase()

  const filtered = useMemo(() =>
    q ? rows.filter((r) => {
      const autoName = (r.context?.auto_name as string | undefined ?? '').toLowerCase()
      const topEntities = JSON.stringify(r.context?.top_entities ?? '').toLowerCase()
      return autoName.includes(q) || topEntities.includes(q) || r.repo.toLowerCase().includes(q)
    }) : rows,
  [rows, q])

  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-400 dark:text-slate-500">
        <CheckCircle className="w-10 h-10 text-emerald-400/70" />
        <p className="text-sm">No pending community naming — all clusters resolved</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 backdrop-blur-sm flex-shrink-0">
        <div className="flex items-center gap-1.5 flex-1 min-w-[160px] max-w-xs bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-2.5 py-1.5">
          <Search className="w-3.5 h-3.5 text-slate-400 dark:text-slate-500 shrink-0" />
          <input
            type="search"
            aria-label="Search community naming candidates"
            placeholder="Search clusters…"
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            className="flex-1 bg-transparent text-sm text-slate-700 dark:text-slate-300 placeholder-slate-400 dark:placeholder-slate-500 outline-none"
          />
        </div>
        <span className="ml-auto text-xs text-slate-500 dark:text-slate-400 tabular-nums">
          {filtered.length} of {rows.length} clusters
        </span>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto">
        {filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 gap-2 text-slate-400 dark:text-slate-500">
            <Search className="w-8 h-8 text-slate-300" />
            <p className="text-sm">No clusters match "{searchQ}"</p>
          </div>
        ) : (
          filtered.map((row) => {
            const autoName = row.context?.auto_name as string | undefined
            const size = row.context?.size as number | undefined
            const topEntities = (row.context?.top_entities as string[] | undefined) ?? []
            return (
              <div
                key={row.candidate_id}
                className="flex items-start gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50/60 dark:hover:bg-slate-800/40 transition-colors"
              >
                <Network className="w-4 h-4 mt-0.5 shrink-0 text-violet-500 dark:text-violet-400" aria-hidden="true" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-slate-800 dark:text-slate-200 truncate">
                    {autoName ?? row.subject_id}
                    {size != null && (
                      <span className="ml-2 text-xs font-normal text-slate-400 dark:text-slate-500">
                        {size} nodes
                      </span>
                    )}
                  </p>
                  {topEntities.length > 0 && (
                    <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 truncate">
                      {topEntities.slice(0, 5).join(', ')}
                      {topEntities.length > 5 && ` +${topEntities.length - 5} more`}
                    </p>
                  )}
                  {row.repo && (
                    <span className="text-xs font-mono text-slate-400 dark:text-slate-500">{row.repo}</span>
                  )}
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// PendingRoute — Surface 6
// ─────────────────────────────────────────────────────────────────────────────

type TabId = 'repairs' | 'enrichments' | 'community-naming'

/**
 * Surface 6 — Pending queue.
 * Three tabs:
 *   1. Repair candidates (repair_edge + dynamic_baseurl)
 *   2. Enrichment candidates (describe_entity, classify_domain, …) — entity-level
 *   3. Community naming (name_community) — cluster naming, separated per #1301
 *
 * Enrichments are bucketed into 4 collapsible tiers: Critical / High / Medium / Low.
 */
export function PendingRoute() {
  const { group } = useParams<{ group: string }>()
  const [activeTab, setActiveTab] = useState<TabId>('repairs')
  const [appliedCount, setAppliedCount] = useState(0)

  const repairsQuery = useQuery({
    queryKey: ['repairs', group],
    queryFn: () => fetchRepairs(group ?? ''),
    enabled: !!group,
    staleTime: 30 * 1000,
  })

  const enrichmentsQuery = useQuery({
    queryKey: ['enrichments', group],
    queryFn: () => fetchEnrichments(group ?? ''),
    enabled: !!group,
    staleTime: 30 * 1000,
  })

  const communityNamingQuery = useQuery({
    queryKey: ['community-naming', group],
    queryFn: () => fetchCommunityNaming(group ?? ''),
    enabled: !!group,
    staleTime: 30 * 1000,
  })

  if (!group) {
    return (
      <div className="h-full flex items-center justify-center text-slate-400 dark:text-slate-500">
        <p className="text-sm">No group selected.</p>
      </div>
    )
  }

  const repairCount = repairsQuery.data?.total ?? 0
  const enrichCount = enrichmentsQuery.data?.total ?? 0
  const communityCount = communityNamingQuery.data?.total ?? 0
  const isLoading = repairsQuery.isLoading || enrichmentsQuery.isLoading || communityNamingQuery.isLoading
  const hasError = repairsQuery.isError || enrichmentsQuery.isError || communityNamingQuery.isError

  const tabs: { id: TabId; label: string; count: number; icon: React.ReactNode }[] = [
    {
      id: 'repairs',
      label: 'Repair candidates',
      count: repairCount,
      icon: <Wrench className="w-3.5 h-3.5" />,
    },
    {
      id: 'enrichments',
      label: 'Enrichment candidates',
      count: enrichCount,
      icon: <Sparkles className="w-3.5 h-3.5" />,
    },
    {
      id: 'community-naming',
      label: 'Community naming',
      count: communityCount,
      icon: <Network className="w-3.5 h-3.5" />,
    },
  ]

  return (
    <div className="flex flex-col h-full overflow-hidden bg-white dark:bg-slate-950">
      {/* ── Tab bar ─────────────────────────────────────────────────────────── */}
      <div className="flex items-center gap-0 border-b border-slate-200 dark:border-slate-800 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm flex-shrink-0 px-4">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={activeTab === tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={[
              'flex items-center gap-1.5 px-3 py-3 text-sm font-medium border-b-2 transition-colors -mb-px',
              activeTab === tab.id
                ? 'border-sky-500 text-sky-600 dark:text-sky-400'
                : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300',
            ].join(' ')}
          >
            {tab.icon}
            {tab.label}
            {!isLoading && (
              <span className={[
                'ml-1 px-1.5 py-0.5 rounded-full text-xs font-semibold tabular-nums',
                tab.count > 0
                  ? 'bg-sky-100 dark:bg-sky-900/50 text-sky-700 dark:text-sky-300'
                  : 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500',
              ].join(' ')}>
                {tab.count}
              </span>
            )}
          </button>
        ))}

        {appliedCount > 0 && (
          <span className="ml-auto text-xs text-emerald-600 dark:text-emerald-400 tabular-nums">
            {appliedCount} actioned this session
          </span>
        )}
      </div>

      {/* ── Content ─────────────────────────────────────────────────────────── */}
      <div className="flex-1 overflow-hidden flex flex-col">
        {hasError && (
          <div className="flex items-center gap-2 px-4 py-3 m-4 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-sm text-red-700 dark:text-red-400">
            <AlertCircle className="w-4 h-4 shrink-0" />
            Failed to load pending candidates. Is the daemon running?
          </div>
        )}

        {isLoading && !hasError && (
          <div className="overflow-y-auto flex-1">
            {Array.from({ length: 8 }).map((_, i) => <SkeletonRow key={i} />)}
          </div>
        )}

        {!isLoading && !hasError && activeTab === 'repairs' && (
          <div className="flex-1 overflow-y-auto">
            {(repairsQuery.data?.items ?? []).length === 0 ? (
              <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-400 dark:text-slate-500">
                <CheckCircle className="w-10 h-10 text-emerald-400/70" />
                <p className="text-sm">No pending repairs — graph is fully resolved</p>
              </div>
            ) : (
              (repairsQuery.data?.items ?? []).map((row) => (
                <div
                  key={row.candidate_id}
                  className="flex items-start gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50/60 dark:hover:bg-slate-800/40 transition-colors group"
                >
                  <span className="shrink-0 mt-0.5 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border border-sky-200 dark:border-sky-700">
                    {row.kind}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-slate-800 dark:text-slate-200 truncate">
                      {subjectLabel(row)}
                    </p>
                    <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 truncate">
                      {contextSummary(row)}
                      {row.repo && <span className="ml-2 font-mono text-slate-400">{row.repo}</span>}
                    </p>
                  </div>
                  {row.confidence != null && row.confidence > 0 && (
                    <span className="shrink-0 text-xs text-slate-400 tabular-nums mt-1">
                      {(row.confidence * 100).toFixed(0)}%
                    </span>
                  )}
                </div>
              ))
            )}
          </div>
        )}

        {!isLoading && !hasError && activeTab === 'enrichments' && (
          <div className="flex flex-col flex-1 overflow-hidden">
            {/* Per-tier live progress (#1286) */}
            <EnrichmentProgressPanel group={group} />
            <TieredList
              rows={enrichmentsQuery.data?.items ?? []}
              group={group}
              emptyMessage="No pending enrichments — all candidates resolved"
              onApplied={() => setAppliedCount((n) => n + 1)}
            />
          </div>
        )}

        {!isLoading && !hasError && activeTab === 'community-naming' && (
          <div className="flex flex-col flex-1 overflow-hidden">
            <CommunityNamingList rows={communityNamingQuery.data?.items ?? []} />
          </div>
        )}
      </div>
    </div>
  )
}
