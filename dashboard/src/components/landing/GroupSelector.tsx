/**
 * GroupSelector — landing page card grid.
 *
 * Shows all indexed groups as cards with name, sparkline, repo count,
 * entity count, bug-rate and last-indexed time. Clicking a card navigates
 * to /graph/<group>.
 */

import { useNavigate } from 'react-router-dom'
import { Database, GitBranch, Clock, TerminalSquare } from 'lucide-react'
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
  if (rate === undefined) return 'text-slate-500'
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

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
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
      className="rounded-xl border border-slate-800 bg-slate-900/60 p-5 space-y-4"
      role="status"
      aria-label="Loading group…"
    >
      {/* header row */}
      <div className="flex items-center justify-between">
        <div className="h-5 w-32 rounded bg-slate-800 animate-pulse" />
        <div className="h-6 w-16 rounded bg-slate-800 animate-pulse" />
      </div>
      {/* stats row */}
      <div className="flex items-center gap-4">
        <div className="h-4 w-12 rounded bg-slate-800 animate-pulse" />
        <div className="h-4 w-16 rounded bg-slate-800 animate-pulse" />
        <div className="h-4 w-14 rounded bg-slate-800 animate-pulse" />
        <div className="h-4 w-16 rounded bg-slate-800 animate-pulse" />
      </div>
      <span className="sr-only">Loading…</span>
    </div>
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
    group.bug_rate !== undefined ? `${group.bug_rate.toFixed(1)}%` : '—'

  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'w-full text-left rounded-xl border bg-slate-900/60 p-5 space-y-3',
        'transition-all duration-150 cursor-pointer',
        'border-slate-800 hover:border-sky-500/40 hover:-translate-y-px hover:bg-slate-900',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500/60',
      ].join(' ')}
    >
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

      {/* Stats row */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-slate-400">
        {/* Repo count */}
        <span className="flex items-center gap-1">
          <Database className="w-3.5 h-3.5" aria-hidden />
          <span>{group.repos.length} repo{group.repos.length !== 1 ? 's' : ''}</span>
        </span>

        {/* Entity count */}
        <span className="flex items-center gap-1">
          <TerminalSquare className="w-3.5 h-3.5" aria-hidden />
          <span>{formatCount(group.entity_count)} entities</span>
        </span>

        {/* Bug rate */}
        <span className={`flex items-center gap-1 font-mono ${rateColor}`}>
          <span>{rateLabel}</span>
          <span className="text-slate-500 font-normal">bug-rate</span>
        </span>

        {/* Last indexed */}
        <span className="flex items-center gap-1 ml-auto text-slate-500">
          <Clock className="w-3.5 h-3.5" aria-hidden />
          <span>{relativeTime(group.indexed_at)}</span>
        </span>
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
      <div className="rounded-full bg-slate-800 p-5">
        <GitBranch className="w-10 h-10 text-slate-500" aria-hidden />
      </div>
      <div className="space-y-2">
        <h2 className="text-xl font-semibold text-slate-200">No groups indexed yet</h2>
        <p className="max-w-sm text-sm text-slate-400">
          Register a group and index at least one repo to see it here.
        </p>
      </div>
      <div className="w-full max-w-md space-y-3 text-left">
        <div className="rounded-lg bg-slate-900 border border-slate-800 p-4">
          <p className="text-xs text-slate-500 mb-2 font-mono uppercase tracking-wider">Step 1 — register a group</p>
          <pre className="text-sm text-slate-300 font-mono whitespace-pre-wrap">
            <code>archigraph register --group my-service --path ./my-repo</code>
          </pre>
        </div>
        <div className="rounded-lg bg-slate-900 border border-slate-800 p-4">
          <p className="text-xs text-slate-500 mb-2 font-mono uppercase tracking-wider">Step 2 — index the repo</p>
          <pre className="text-sm text-slate-300 font-mono whitespace-pre-wrap">
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
          <div className="h-7 w-48 rounded bg-slate-800 animate-pulse mb-1" />
          <div className="h-4 w-72 rounded bg-slate-800 animate-pulse" />
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
        <h1 className="text-2xl font-bold text-slate-100">Groups</h1>
        <p className="text-sm text-slate-400 mt-1">
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
