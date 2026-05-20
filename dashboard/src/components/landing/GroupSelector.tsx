/**
 * GroupSelector — landing page card grid.
 *
 * Shows all indexed groups as cards with name, sparkline, repo count,
 * entity count, unresolved edges and last-indexed time. Clicking a card navigates
 * to /graph/<group>.
 */

import { useNavigate } from 'react-router-dom'
import { Database, GitBranch, Clock, TerminalSquare, Activity } from 'lucide-react'
import { useRegistry } from '@/hooks/shared/useRegistry'
import type { GroupMeta } from '@/types/api'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function relativeTime(iso?: string): string {
  if (!iso) return 'never'
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  return `${days}d ago`
}

function bugRateColor(rate?: number): string {
  if (rate === undefined) return 'text-slate-400 dark:text-slate-500'
  if (rate <= 5) return 'text-emerald-400'
  if (rate <= 15) return 'text-amber-400'
  return 'text-red-400'
}

function bugRateDotColor(rate?: number): string {
  if (rate === undefined) return '#64748b'
  if (rate <= 5) return '#34d399'
  if (rate <= 15) return '#fbbf24'
  return '#f87171'
}

/** Format with thousands separator, abbreviating at 1 M+ */
function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return n.toLocaleString()
  return String(n)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tooltip wrapper (title-based; CSS tooltip for richer UX)
// ─────────────────────────────────────────────────────────────────────────────

interface TooltipProps {
  tip: string
  children: React.ReactNode
}

function Tooltip({ tip, children }: TooltipProps) {
  return (
    <span className="relative group/tip inline-flex items-center">
      {children}
      <span
        role="tooltip"
        className={[
          'pointer-events-none absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 z-20',
          'whitespace-nowrap rounded bg-slate-300 dark:bg-slate-700 px-2 py-1 text-xs text-slate-800 dark:text-slate-200 shadow-lg',
          'opacity-0 group-hover/tip:opacity-100 transition-opacity duration-150',
        ].join(' ')}
      >
        {tip}
      </span>
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Sparkline (60×24 SVG)
// ─────────────────────────────────────────────────────────────────────────────

interface SparklineProps {
  history?: number[]
  bugRate?: number
  width?: number
  height?: number
}

function Sparkline({ history, bugRate, width = 60, height = 24 }: SparklineProps) {
  if (!history || history.length < 2) {
    return (
      <svg width={width} height={height} aria-hidden>
        <line x1={0} y1={height / 2} x2={width} y2={height / 2} stroke="#334155" strokeWidth={1.5} />
      </svg>
    )
  }

  const pad = 2
  const min = Math.min(...history)
  const max = Math.max(...history)
  const range = max - min || 1

  const pts = history.map((v, i) => {
    const x = pad + (i / (history.length - 1)) * (width - pad * 2)
    const y = pad + (1 - (v - min) / range) * (height - pad * 2)
    return [x, y] as [number, number]
  })

  const d = pts.map(([x, y], i) => `${i === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const dotColor = bugRateDotColor(bugRate)
  const [lastX, lastY] = pts[pts.length - 1]

  return (
    <svg width={width} height={height} aria-hidden>
      <path d={d} fill="none" stroke="#475569" strokeWidth={1.5} strokeLinejoin="round" />
      <circle cx={lastX} cy={lastY} r={3} fill={dotColor} />
    </svg>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Skeleton card
// ─────────────────────────────────────────────────────────────────────────────

function GroupCardSkeleton() {
  return (
    <div
      className="rounded-xl border border-slate-800 bg-slate-900/60 p-5 h-[232px] space-y-4"
      role="status"
      aria-label="Loading group…"
    >
      {/* header row */}
      <div className="flex items-center justify-between">
        <div className="h-5 w-32 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
        <div className="h-6 w-16 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
      </div>
      {/* stats row */}
      <div className="flex items-center gap-4">
        <div className="h-4 w-12 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
        <div className="h-4 w-16 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
        <div className="h-4 w-14 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
        <div className="h-4 w-16 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
      </div>
      {/* footer skeleton */}
      <div className="border-t border-slate-800/60 pt-3 space-y-2">
        <div className="flex gap-1">
          <div className="h-4 w-14 rounded bg-slate-800 animate-pulse" />
          <div className="h-4 w-12 rounded bg-slate-800 animate-pulse" />
          <div className="h-4 w-10 rounded bg-slate-800 animate-pulse" />
        </div>
        <div className="h-3 w-36 rounded bg-slate-800 animate-pulse" />
      </div>
      <span className="sr-only">Loading…</span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Stat pill — icon + label + value (with tooltip on icon)
// ─────────────────────────────────────────────────────────────────────────────

interface StatPillProps {
  icon: React.ReactNode
  tooltip: string
  label: string
  value: string
  valueClass?: string
  dimmed?: boolean
}

function StatPill({ icon, tooltip, label, value, valueClass = 'text-slate-700 dark:text-slate-300', dimmed = false }: StatPillProps) {
  return (
    <span className={`flex items-center gap-1 min-w-0 ${dimmed ? 'opacity-40' : ''}`}>
      <Tooltip tip={tooltip}>
        <span className="text-slate-400 dark:text-slate-500 shrink-0">{icon}</span>
      </Tooltip>
      <span className="text-slate-400 dark:text-slate-500 text-xs shrink-0">{label}:</span>
      <span className={`text-xs font-medium truncate ${valueClass}`}>{value}</span>
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// FrameworkChips — Row 1 of the card footer
// ─────────────────────────────────────────────────────────────────────────────

const MAX_VISIBLE_CHIPS = 4

interface FrameworkChipsProps {
  frameworks?: string[]
}

function FrameworkChips({ frameworks }: FrameworkChipsProps) {
  if (!frameworks || frameworks.length === 0) {
    return <div className="flex items-center gap-1 h-5" aria-label="No frameworks detected" />
  }

  const visible = frameworks.slice(0, MAX_VISIBLE_CHIPS)
  const overflow = frameworks.slice(MAX_VISIBLE_CHIPS)
  const overflowTip = overflow.join(', ')

  return (
    <div className="flex items-center gap-1 flex-wrap min-h-[20px]" role="list" aria-label="Frameworks">
      {visible.map((fw) => (
        <span
          key={fw}
          role="listitem"
          className="inline-block px-1.5 py-0.5 rounded text-[10px] font-mono leading-none bg-slate-700 text-sky-400"
        >
          {fw}
        </span>
      ))}
      {overflow.length > 0 && (
        <Tooltip tip={overflowTip}>
          <span
            data-overflow-chip
            className="inline-block px-1.5 py-0.5 rounded text-[10px] font-mono leading-none cursor-default bg-slate-700 text-sky-400"
          >
            +{overflow.length} more
          </span>
        </Tooltip>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// RepoRow — Row 2 of the card footer
// ─────────────────────────────────────────────────────────────────────────────

interface RepoRowProps {
  repos: Array<{ slug: string }>
}

function RepoRow({ repos }: RepoRowProps) {
  const names = repos.map((r) => r.slug)
  const joined = names.join(', ')
  const tip = names.length > 1 ? joined : ''

  return (
    <Tooltip tip={tip}>
      <span
        data-repo-row
        className="block text-[10px] text-slate-500 truncate w-full leading-snug"
        aria-label={`Repos: ${joined}`}
      >
        {joined}
      </span>
    </Tooltip>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Group card
// ─────────────────────────────────────────────────────────────────────────────

interface GroupCardProps {
  group: GroupMeta
  onClick: () => void
}

function GroupCard({ group, onClick }: GroupCardProps) {
  const rateColor = bugRateColor(group.bug_rate)
  const rateLabel =
    group.bug_rate !== undefined ? `${group.bug_rate.toFixed(1)}%` : 'not measured'
  const hasRealData = group.entity_count > 0
  const hasIndexedAt = Boolean(group.indexed_at)

  return (
    <button
      type="button"
      data-card
      onClick={onClick}
      className={[
        'w-full text-left rounded-xl border bg-slate-900/60 p-5',
        // Fixed height: 152px (stats) + 80px (footer) = 232px.
        // All cards identical height regardless of framework count.
        'h-[232px] flex flex-col',
        'transition-all duration-150 cursor-pointer',
        'border-slate-200 dark:border-slate-800 hover:border-sky-500/40 hover:-translate-y-px hover:bg-slate-200 dark:hover:bg-slate-900',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500/60',
      ].join(' ')}
    >
      {/* Main content — grows to fill available space */}
      <div className="flex-1 flex flex-col justify-between min-h-0">
        {/* Header: group name + sparkline */}
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2 min-w-0">
            <GitBranch className="w-4 h-4 text-sky-400 shrink-0" aria-hidden />
            <span className="text-xl font-semibold text-slate-100 truncate">
              {group.display_name}
            </span>
          </div>
          <div className="shrink-0 pt-0.5">
            <Sparkline history={group.bug_rate_history} bugRate={group.bug_rate} />
          </div>
        </div>

        {/* Stats grid — 2×2 to keep consistent height */}
        <div className="grid grid-cols-2 gap-x-3 gap-y-2">
          {/* Repos */}
          <StatPill
            icon={<Database className="w-3.5 h-3.5" aria-hidden />}
            tooltip="Number of repositories in this group"
            label="Repos"
            value={String(group.repos.length)}
          />

          {/* Entities */}
          <StatPill
            icon={<TerminalSquare className="w-3.5 h-3.5" aria-hidden />}
            tooltip="Total entities across all repos"
            label="Entities"
            value={formatCount(group.entity_count)}
            dimmed={!hasRealData}
          />

          {/* Unresolved edges */}
          <StatPill
            icon={<Activity className="w-3.5 h-3.5" aria-hidden />}
            tooltip="Percentage of graph edges that point to unknown or ambiguous targets. Lower is better. Drops as the resolver improves."
            label="Unresolved edges"
            value={rateLabel}
            valueClass={`text-xs font-medium font-mono ${rateColor}`}
            dimmed={group.bug_rate === undefined}
          />

          {/* Last indexed */}
          <StatPill
            icon={<Clock className="w-3.5 h-3.5" aria-hidden />}
            tooltip="Time since most recent indexing"
            label="Indexed"
            value={relativeTime(group.indexed_at)}
            dimmed={!hasIndexedAt}
          />
        </div>
      </div>

      {/* Footer — fixed 80px: framework chips + repo names */}
      <div className="h-[80px] flex flex-col justify-center gap-1.5 pt-3 border-t border-slate-800/60 mt-3 shrink-0">
        <FrameworkChips frameworks={group.frameworks} />
        <RepoRow repos={group.repos} />
      </div>
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty state
// ─────────────────────────────────────────────────────────────────────────────

function NoGroupsState() {
  return (
    <div className="flex flex-col items-center justify-center gap-6 py-24 px-8 text-center">
      <div className="rounded-full bg-slate-200 dark:bg-slate-800 p-5">
        <GitBranch className="w-10 h-10 text-slate-400 dark:text-slate-500" aria-hidden />
      </div>
      <div className="space-y-2">
        <h2 className="text-xl font-semibold text-slate-800 dark:text-slate-200">No groups indexed yet</h2>
        <p className="max-w-sm text-sm text-slate-400 dark:text-slate-400">
          Register a group and index at least one repo to see it here.
        </p>
      </div>
      <div className="w-full max-w-md space-y-3 text-left">
        <div className="rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 p-4">
          <p className="text-xs text-slate-400 dark:text-slate-500 mb-2 font-mono uppercase tracking-wider">Step 1 — register a group</p>
          <pre className="text-sm text-slate-700 dark:text-slate-300 font-mono whitespace-pre-wrap">
            <code>archigraph register --group my-service --path ./my-repo</code>
          </pre>
        </div>
        <div className="rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 p-4">
          <p className="text-xs text-slate-400 dark:text-slate-500 mb-2 font-mono uppercase tracking-wider">Step 2 — index the repo</p>
          <pre className="text-sm text-slate-700 dark:text-slate-300 font-mono whitespace-pre-wrap">
            <code>archigraph index my-service</code>
          </pre>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// GroupSelector (public)
// ─────────────────────────────────────────────────────────────────────────────

export function GroupSelector() {
  const navigate = useNavigate()
  const { data: registry, isLoading, isError } = useRegistry()

  if (isLoading) {
    return (
      <div className="px-6 py-8 max-w-7xl mx-auto">
        <div className="mb-6">
          <div className="h-7 w-48 rounded bg-slate-200 dark:bg-slate-800 animate-pulse mb-1" />
          <div className="h-4 w-72 rounded bg-slate-200 dark:bg-slate-800 animate-pulse" />
        </div>
        <div
          className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4"
          role="status"
          aria-label="Loading groups…"
        >
          {Array.from({ length: 4 }, (_, i) => <GroupCardSkeleton key={i} />)}
        </div>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-24 text-center">
        <p className="text-sm text-red-400">Failed to load groups. Is the archigraph daemon running?</p>
      </div>
    )
  }

  const groups = registry?.groups ?? []

  if (groups.length === 0) {
    return <NoGroupsState />
  }

  return (
    <div className="px-6 py-8 max-w-7xl mx-auto">
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-100">Groups</h1>
        <p className="text-sm text-slate-400 dark:text-slate-400 mt-1">
          Select a group to explore its graph, flows, topology, paths and docs.
        </p>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {groups.map((group) => (
          <GroupCard
            key={group.id}
            group={group}
            onClick={() => navigate(`/graph/${group.id}`)}
          />
        ))}
      </div>
    </div>
  )
}
