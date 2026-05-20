import { useNavigate } from 'react-router-dom'
import { AlertTriangle, ExternalLink, Loader2 } from 'lucide-react'
import type { OrphanCaller, OrphanCallerReason } from '@/types/api'

// ── Severity order for sorting ────────────────────────────────────────────────
const REASON_SEVERITY: Record<OrphanCallerReason, number> = {
  no_handler_found: 0,
  dynamic_baseurl: 1,
  template_literal: 2,
}

function sortBySeverity(callers: OrphanCaller[]): OrphanCaller[] {
  return [...callers].sort(
    (a, b) => REASON_SEVERITY[a.reason] - REASON_SEVERITY[b.reason],
  )
}

// ── Reason badge ──────────────────────────────────────────────────────────────

const REASON_LABEL: Record<OrphanCallerReason, string> = {
  no_handler_found: 'no handler',
  dynamic_baseurl: 'dynamic baseURL',
  template_literal: 'template literal',
}

const REASON_COLOR: Record<OrphanCallerReason, string> = {
  no_handler_found:
    'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
  dynamic_baseurl:
    'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border-amber-200 dark:border-amber-700',
  template_literal:
    'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
}

function ReasonBadge({ reason }: { reason: OrphanCallerReason }) {
  return (
    <span
      className={[
        'inline-flex items-center px-1.5 py-0.5 text-[10px] font-mono rounded border',
        REASON_COLOR[reason],
      ].join(' ')}
    >
      {REASON_LABEL[reason]}
    </span>
  )
}

// ── Method chip ───────────────────────────────────────────────────────────────

const METHOD_COLOR: Record<string, string> = {
  GET: 'text-emerald-600 dark:text-emerald-400',
  POST: 'text-sky-600 dark:text-sky-400',
  PUT: 'text-amber-600 dark:text-amber-400',
  PATCH: 'text-violet-600 dark:text-violet-400',
  DELETE: 'text-red-600 dark:text-red-400',
}

function MethodChip({ method }: { method: string }) {
  const color = METHOD_COLOR[method.toUpperCase()] ?? 'text-slate-500 dark:text-slate-400'
  return (
    <span className={['font-mono text-xs font-semibold w-14 flex-shrink-0', color].join(' ')}>
      {method.toUpperCase()}
    </span>
  )
}

// ── Individual caller row ─────────────────────────────────────────────────────

interface CallerRowProps {
  caller: OrphanCaller
  group: string
}

function CallerRow({ caller, group }: CallerRowProps) {
  const navigate = useNavigate()
  const fileName = caller.caller_file.split('/').pop() ?? caller.caller_file

  const handleClick = () => {
    // Navigate to Pending surface with this candidate pre-selected.
    // The pending surface (#1010) accepts an `id` query param for pre-selection.
    navigate(`/pending/${group}?id=${encodeURIComponent(caller.id)}`)
  }

  return (
    <div
      role="row"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          handleClick()
        }
      }}
      className="group flex items-center gap-3 px-4 py-2.5 border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-900/60 cursor-pointer focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500"
    >
      {/* Method */}
      <MethodChip method={caller.method} />

      {/* URL pattern */}
      <span className="flex-1 min-w-0 font-mono text-xs text-slate-700 dark:text-slate-200 truncate" title={caller.url_pattern}>
        {caller.url_pattern}
      </span>

      {/* Reason badge */}
      <ReasonBadge reason={caller.reason} />

      {/* Caller file + line */}
      <span
        className="text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 tabular-nums max-w-[180px] truncate text-right"
        title={`${caller.caller_file}:${caller.caller_line}`}
      >
        {fileName}:{caller.caller_line}
      </span>

      {/* Navigate hint */}
      <ExternalLink className="w-3.5 h-3.5 text-slate-300 dark:text-slate-600 group-hover:text-sky-400 flex-shrink-0 transition-colors" aria-hidden />
    </div>
  )
}

// ── Empty state ───────────────────────────────────────────────────────────────

function EmptyOrphans({ backendPending }: { backendPending: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <AlertTriangle className="w-8 h-8 text-slate-300 dark:text-slate-600" />
      {backendPending ? (
        <>
          <p className="text-sm text-slate-500 dark:text-slate-400 font-medium">
            Backend not yet deployed
          </p>
          <p className="text-xs text-slate-400 dark:text-slate-500 max-w-xs">
            The orphan-callers detector is landing via #1091.
            Once deployed, unmatched frontend FETCH call sites will appear here.
          </p>
        </>
      ) : (
        <>
          <p className="text-sm text-slate-500 dark:text-slate-400 font-medium">
            No orphan callers found
          </p>
          <p className="text-xs text-slate-400 dark:text-slate-500 max-w-xs">
            All frontend FETCH call sites have a matching backend handler.
          </p>
        </>
      )}
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

interface OrphanCallersTabProps {
  group: string
  callers: OrphanCaller[]
  isLoading: boolean
  /** True when backend returned 404 (endpoint not yet deployed) */
  backendPending: boolean
}

export function OrphanCallersTab({
  group,
  callers,
  isLoading,
  backendPending,
}: OrphanCallersTabProps) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16 text-slate-400 dark:text-slate-500 gap-2">
        <Loader2 className="w-4 h-4 animate-spin" aria-hidden />
        <span className="text-sm">Loading orphan callers…</span>
      </div>
    )
  }

  if (callers.length === 0) {
    return <EmptyOrphans backendPending={backendPending} />
  }

  const sorted = sortBySeverity(callers)

  return (
    <div
      role="grid"
      aria-label="Orphan caller list"
      className="flex-1 overflow-y-auto"
    >
      {/* Column header */}
      <div className="flex items-center gap-3 px-4 py-1.5 border-b border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/50 text-[10px] text-slate-400 dark:text-slate-500 uppercase tracking-wide">
        <span className="w-14 flex-shrink-0">Method</span>
        <span className="flex-1">URL pattern</span>
        <span>Reason</span>
        <span className="text-right flex-shrink-0 w-[180px]">Caller</span>
        <span className="w-3.5 flex-shrink-0" />
      </div>

      {sorted.map((caller) => (
        <CallerRow key={caller.id} caller={caller} group={group} />
      ))}
    </div>
  )
}
