import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchRepairs, fetchEnrichments, postCandidateAction } from '@/api/client'
import type { PendingCandidateRow } from '@/types/api'
import { Wrench, Sparkles, CheckCircle, XCircle, AlertCircle } from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Skeleton row — displayed while data loads
// ─────────────────────────────────────────────────────────────────────────────

function SkeletonRow() {
  return (
    <div className="animate-pulse flex items-center gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800">
      <div className="h-4 w-24 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-4 w-40 bg-slate-200 dark:bg-slate-700 rounded flex-1" />
      <div className="h-4 w-16 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-6 w-14 bg-slate-200 dark:bg-slate-700 rounded" />
      <div className="h-6 w-14 bg-slate-200 dark:bg-slate-700 rounded" />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

/** Extract a human-readable "subject" label from a candidate row. */
function subjectLabel(row: PendingCandidateRow): string {
  const ctx = row.context ?? {}
  // repair_edge / dynamic_baseurl_endpoint
  if (typeof ctx.original_stub === 'string' && ctx.original_stub) return ctx.original_stub
  if (typeof ctx.path === 'string' && ctx.path) return ctx.path
  // describe_entity / classify_domain / describe_role
  if (typeof ctx.name === 'string' && ctx.name) return ctx.name
  // name_community
  if (typeof ctx.auto_name === 'string' && ctx.auto_name) return ctx.auto_name
  return row.subject_id
}

/** Extract a short context summary for display. */
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

/** Extract a proposed value from context if present. */
function proposedValue(row: PendingCandidateRow): string | null {
  const ctx = row.context ?? {}
  if (typeof ctx.proposed_value === 'string' && ctx.proposed_value) return ctx.proposed_value
  if (typeof ctx.static_path_suffix === 'string' && ctx.static_path_suffix) return ctx.static_path_suffix
  return null
}

// ─────────────────────────────────────────────────────────────────────────────
// ScoreBadge — displays the 0–100 confidence score with criticality colouring
// ─────────────────────────────────────────────────────────────────────────────

interface ScoreBadgeProps {
  score: number
  band?: string
  breakdown?: string
}

function ScoreBadge({ score, band, breakdown }: ScoreBadgeProps) {
  const label = band
    ? `${band.charAt(0).toUpperCase()}${band.slice(1)} ${score}`
    : `${score}`

  const colorClass =
    band === 'critical'
      ? 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700'
      : band === 'high'
        ? 'bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 border-orange-200 dark:border-orange-700'
        : band === 'medium'
          ? 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700'
          : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700'

  return (
    <span
      className={`shrink-0 mt-0.5 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold border ${colorClass}`}
      title={breakdown ?? `Score: ${score}`}
    >
      {label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// CandidateRow component
// ─────────────────────────────────────────────────────────────────────────────

interface CandidateRowProps {
  row: PendingCandidateRow
  group: string
  onApplied: () => void
}

function CandidateRow({ row, group, onApplied }: CandidateRowProps) {
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
    <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50/60 dark:hover:bg-slate-800/40 transition-colors group">
      {/* Kind badge */}
      <span className="shrink-0 mt-0.5 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border border-sky-200 dark:border-sky-700">
        {row.kind}
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
      </div>

      {/* Score badge (issue #1131) — shown when a 0–100 score is present */}
      {row.score != null && row.score > 0 ? (
        <ScoreBadge score={row.score} band={row.criticality_band} breakdown={row.score_breakdown} />
      ) : row.confidence != null && row.confidence > 0 ? (
        <span className="shrink-0 text-xs text-slate-400 dark:text-slate-500 tabular-nums mt-1">
          {(row.confidence * 100).toFixed(0)}%
        </span>
      ) : null}

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
// CandidateList
// ─────────────────────────────────────────────────────────────────────────────

interface CandidateListProps {
  rows: PendingCandidateRow[]
  group: string
  emptyMessage: string
  onApplied: () => void
}

function CandidateList({ rows, group, emptyMessage, onApplied }: CandidateListProps) {
  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-400 dark:text-slate-500">
        <CheckCircle className="w-10 h-10 text-emerald-400/70" />
        <p className="text-sm">{emptyMessage}</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col">
      {rows.map((row) => (
        <CandidateRow
          key={row.candidate_id}
          row={row}
          group={group}
          onApplied={onApplied}
        />
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// PendingRoute — Surface 6
// ─────────────────────────────────────────────────────────────────────────────

type TabId = 'repairs' | 'enrichments'

/**
 * Surface 6 — Pending queue.
 * Two tabs: Repair candidates (repair_edge + dynamic_baseurl) and
 * Enrichment candidates (describe_entity, classify_domain, …).
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

  if (!group) {
    return (
      <div className="h-full flex items-center justify-center text-slate-400 dark:text-slate-500">
        <p className="text-sm">No group selected.</p>
      </div>
    )
  }

  const repairCount = repairsQuery.data?.total ?? 0
  const enrichCount = enrichmentsQuery.data?.total ?? 0
  const isLoading = repairsQuery.isLoading || enrichmentsQuery.isLoading
  const hasError = repairsQuery.isError || enrichmentsQuery.isError

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

        {/* Applied counter feedback */}
        {appliedCount > 0 && (
          <span className="ml-auto text-xs text-emerald-600 dark:text-emerald-400 tabular-nums">
            {appliedCount} actioned this session
          </span>
        )}
      </div>

      {/* ── Content ─────────────────────────────────────────────────────────── */}
      <div className="flex-1 overflow-y-auto">
        {hasError && (
          <div className="flex items-center gap-2 px-4 py-3 m-4 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-sm text-red-700 dark:text-red-400">
            <AlertCircle className="w-4 h-4 shrink-0" />
            Failed to load pending candidates. Is the daemon running?
          </div>
        )}

        {isLoading && !hasError && (
          <div>
            {Array.from({ length: 8 }).map((_, i) => <SkeletonRow key={i} />)}
          </div>
        )}

        {!isLoading && !hasError && activeTab === 'repairs' && (
          <CandidateList
            rows={repairsQuery.data?.items ?? []}
            group={group}
            emptyMessage="No pending repairs — graph is fully resolved"
            onApplied={() => setAppliedCount((n) => n + 1)}
          />
        )}

        {!isLoading && !hasError && activeTab === 'enrichments' && (
          <CandidateList
            rows={enrichmentsQuery.data?.items ?? []}
            group={group}
            emptyMessage="No pending enrichments — all candidates resolved"
            onApplied={() => setAppliedCount((n) => n + 1)}
          />
        )}
      </div>
    </div>
  )
}
