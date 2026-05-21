import { useState, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import {
  fetchQualityOrphans, fetchQualityFixtures, postQualityRecall,
  fetchQualityHistory,
  type OrphanAuditReply, type RecallReply, type KindStat, type RepoOrphanStats,
  type HealthEntry,
  type QualityHistoryReply,
} from '@/api/client'
import {
  ShieldCheck, ShieldAlert, PlayCircle, RefreshCw,
  AlertTriangle, CheckCircle2, XCircle, ChevronDown, ChevronUp,
  Download, BarChart3,
  Activity, TrendingDown, TrendingUp, Minus,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function pct(v: number) {
  return `${(v * 100).toFixed(1)}%`
}

function fmt1(n: number) {
  return n.toFixed(1)
}

function fmtDate(iso: string) {
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function fmtDateFull(iso: string) {
  const d = new Date(iso)
  return d.toLocaleString()
}

/** Clamp n to [lo, hi]. */
function clamp(n: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, n))
}

function healthColor(score: number) {
  if (score >= 80) return 'text-emerald-600 dark:text-emerald-400'
  if (score >= 60) return 'text-yellow-600 dark:text-yellow-400'
  return 'text-red-600 dark:text-red-400'
}

function rateColor(rate: number) {
  if (rate < 0.05) return 'bg-emerald-500'
  if (rate < 0.15) return 'bg-yellow-400'
  return 'bg-red-500'
}

function downloadJSON(data: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

// ─────────────────────────────────────────────────────────────────────────────
// Velocity badge
// ─────────────────────────────────────────────────────────────────────────────

function VelocityBadge({ delta }: { delta: number }) {
  if (Math.abs(delta) < 0.5) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-slate-400 dark:text-slate-500">
        <Minus className="w-3 h-3" />
        stable
      </span>
    )
  }
  if (delta > 0) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400 font-medium">
        <TrendingUp className="w-3 h-3" />
        +{fmt1(delta)}pts
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs text-red-500 dark:text-red-400 font-medium">
      <TrendingDown className="w-3 h-3" />
      {fmt1(delta)}pts
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// SVG trend chart
// ─────────────────────────────────────────────────────────────────────────────

interface TooltipState {
  x: number
  y: number
  entry: HealthEntry
}

interface TrendChartProps {
  entries: HealthEntry[]
  width?: number
  height?: number
}

function TrendChart({ entries, width = 640, height = 140 }: TrendChartProps) {
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)

  if (entries.length === 0) return null

  const PAD = { top: 12, right: 16, bottom: 28, left: 36 }
  const innerW = width - PAD.left - PAD.right
  const innerH = height - PAD.top - PAD.bottom

  // Map entries to pixel coords.
  const scores = entries.map((e) => e.health_score)
  const minScore = Math.max(0, Math.floor(Math.min(...scores) / 10) * 10 - 10)
  const maxScore = Math.min(100, Math.ceil(Math.max(...scores) / 10) * 10 + 10)
  const scoreRange = Math.max(1, maxScore - minScore)

  const toX = (i: number) =>
    PAD.left + (entries.length === 1 ? innerW / 2 : (i / (entries.length - 1)) * innerW)

  const toY = (score: number) =>
    PAD.top + innerH - ((score - minScore) / scoreRange) * innerH

  const points = entries.map((e, i) => ({ x: toX(i), y: toY(e.health_score), entry: e }))

  const polyline = points.map((p) => `${p.x},${p.y}`).join(' ')

  // Y-axis ticks.
  const yTicks: number[] = []
  for (let v = minScore; v <= maxScore; v += 20) yTicks.push(v)

  // Health-score colour: green ≥ 80, yellow ≥ 60, red otherwise.
  const lastScore = scores[scores.length - 1]
  const lineColor =
    lastScore >= 80
      ? '#10b981' /* emerald-500 */
      : lastScore >= 60
        ? '#f59e0b' /* amber-500 */
        : '#ef4444' /* red-500 */

  return (
    <div className="relative select-none">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full"
        style={{ height }}
        onMouseLeave={() => setTooltip(null)}
      >
        {/* Y-axis grid lines + labels */}
        {yTicks.map((v) => {
          const cy = toY(v)
          return (
            <g key={v}>
              <line
                x1={PAD.left}
                y1={cy}
                x2={PAD.left + innerW}
                y2={cy}
                stroke="currentColor"
                strokeWidth={0.5}
                className="text-slate-200 dark:text-slate-700"
                strokeDasharray="3 3"
              />
              <text
                x={PAD.left - 4}
                y={cy}
                textAnchor="end"
                dominantBaseline="middle"
                fontSize={9}
                className="fill-slate-400 dark:fill-slate-500"
              >
                {v}
              </text>
            </g>
          )
        })}

        {/* X-axis date labels (show first + last + up to 3 evenly spaced) */}
        {entries.map((e, i) => {
          const total = entries.length
          const show =
            i === 0 ||
            i === total - 1 ||
            (total > 2 && i % Math.max(1, Math.floor(total / 4)) === 0)
          if (!show) return null
          return (
            <text
              key={i}
              x={toX(i)}
              y={PAD.top + innerH + 14}
              textAnchor="middle"
              fontSize={8}
              className="fill-slate-400 dark:fill-slate-500"
            >
              {fmtDate(e.timestamp)}
            </text>
          )
        })}

        {/* Trend line */}
        {entries.length > 1 && (
          <polyline
            points={polyline}
            fill="none"
            stroke={lineColor}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        )}

        {/* Data points + hit targets */}
        {points.map((p, i) => (
          <g key={i}>
            <circle
              cx={p.x}
              cy={p.y}
              r={4}
              fill={lineColor}
              stroke="white"
              strokeWidth={1.5}
              className="dark:stroke-slate-900 cursor-pointer"
            />
            {/* Invisible wider hit target */}
            <circle
              cx={p.x}
              cy={p.y}
              r={10}
              fill="transparent"
              className="cursor-pointer"
              onMouseEnter={() =>
                setTooltip({ x: p.x, y: p.y, entry: p.entry })
              }
            />
          </g>
        ))}
      </svg>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 rounded-md shadow-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-xs px-3 py-2 min-w-[160px]"
          style={{
            left: clamp((tooltip.x / width) * 100, 5, 70) + '%',
            top: tooltip.y < 60 ? '60%' : '5%',
          }}
        >
          <div className="font-semibold text-slate-700 dark:text-slate-200 mb-1">
            {fmtDateFull(tooltip.entry.timestamp)}
          </div>
          <div className="space-y-0.5 text-slate-500 dark:text-slate-400">
            <div>
              Health score:{' '}
              <span className="font-mono font-semibold text-slate-800 dark:text-slate-200">
                {fmt1(tooltip.entry.health_score)}
              </span>
            </div>
            <div>
              Orphan rate:{' '}
              <span className="font-mono">{fmt1(tooltip.entry.orphan_rate)}%</span>
            </div>
            <div>
              Bug rate:{' '}
              <span className="font-mono">{fmt1(tooltip.entry.bug_rate)}%</span>
            </div>
            <div>
              Entities:{' '}
              <span className="font-mono">
                {tooltip.entry.total_entities.toLocaleString()}
              </span>
            </div>
            {tooltip.entry.recall_pct !== undefined && (
              <div>
                Recall:{' '}
                <span className="font-mono">{fmt1(tooltip.entry.recall_pct)}%</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary cards (current / 7d / 30d comparisons)
// ─────────────────────────────────────────────────────────────────────────────

function entryAtOrBefore(entries: HealthEntry[], daysAgo: number): HealthEntry | undefined {
  const cutoff = Date.now() - daysAgo * 24 * 60 * 60 * 1000
  // Walk backwards; find last entry older than cutoff.
  for (let i = entries.length - 1; i >= 0; i--) {
    if (new Date(entries[i].timestamp).getTime() <= cutoff) {
      return entries[i]
    }
  }
  return undefined
}

interface CompareCardProps {
  label: string
  current: number
  reference?: HealthEntry
}

function CompareCard({ label, current, reference }: CompareCardProps) {
  const delta = reference !== undefined ? current - reference.health_score : undefined
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 px-4 py-3 flex flex-col gap-1">
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      {delta !== undefined ? (
        <>
          <div className="font-mono text-2xl font-semibold text-slate-800 dark:text-slate-100">
            {fmt1(reference!.health_score)}
          </div>
          <VelocityBadge delta={current - reference!.health_score} />
        </>
      ) : (
        <div className="font-mono text-sm text-slate-400 dark:text-slate-600 italic">
          no data
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Metric row (orphan / bug rates)
// ─────────────────────────────────────────────────────────────────────────────

function MetricBar({ label, value, danger = 20 }: { label: string; value: number; danger?: number }) {
  const color =
    value <= danger * 0.5
      ? 'bg-emerald-400'
      : value <= danger
        ? 'bg-amber-400'
        : 'bg-red-400'

  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="text-slate-500 dark:text-slate-400 w-28 shrink-0">{label}</span>
      <div className="flex-1 h-2 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${color}`}
          style={{ width: `${clamp(value, 0, 100)}%` }}
        />
      </div>
      <span className="font-mono text-xs text-slate-700 dark:text-slate-300 w-12 text-right">
        {fmt1(value)}%
      </span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV export
// ─────────────────────────────────────────────────────────────────────────────

function downloadCSV(entries: HealthEntry[], group: string) {
  const header = 'timestamp,group,total_entities,orphan_rate,bug_rate,health_score,recall_pct'
  const rows = entries.map((e) =>
    [
      e.timestamp,
      e.group,
      e.total_entities,
      e.orphan_rate,
      e.bug_rate,
      e.health_score,
      e.recall_pct ?? '',
    ].join(','),
  )
  const csv = [header, ...rows].join('\n')
  const blob = new Blob([csv], { type: 'text/csv' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `quality-history-${group}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab type
// ─────────────────────────────────────────────────────────────────────────────

type Tab = 'history' | 'orphans' | 'recall'

// ─────────────────────────────────────────────────────────────────────────────
// History tab (#1214)
// ─────────────────────────────────────────────────────────────────────────────

const DAY_OPTIONS = [7, 30, 90] as const

function HistoryTab({ group }: { group: string }) {
  const [days, setDays] = useState<number>(30)

  const { data, isLoading, isError, refetch, isFetching } = useQuery<QualityHistoryReply>({
    queryKey: ['quality-history', group, days],
    queryFn: () => fetchQualityHistory(group, days),
    staleTime: 60_000,
  })

  const entries = data?.entries ?? []
  const latest = entries.at(-1)

  // Velocity vs reference windows.
  const ref7 = entryAtOrBefore(entries, 7)
  const ref30 = entryAtOrBefore(entries, 30)

  return (
    <div className="space-y-6">
      {/* ── Controls ────────────────────────────────────────────────────────── */}
      <div className="flex items-center justify-between">
        <div className="flex rounded-md border border-slate-200 dark:border-slate-700 overflow-hidden text-xs">
          {DAY_OPTIONS.map((d) => (
            <button
              key={d}
              onClick={() => setDays(d)}
              className={[
                'px-3 py-1.5 transition-colors',
                days === d
                  ? 'bg-sky-50 dark:bg-sky-900/30 text-sky-600 dark:text-sky-400 font-semibold'
                  : 'text-slate-500 hover:bg-slate-50 dark:hover:bg-slate-800',
              ].join(' ')}
            >
              {d}d
            </button>
          ))}
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="p-1.5 rounded text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors disabled:opacity-40"
            aria-label="Refresh"
          >
            <RefreshCw className={`w-4 h-4 ${isFetching ? 'animate-spin' : ''}`} />
          </button>

          {entries.length > 0 && (
            <button
              onClick={() => downloadCSV(entries, group)}
              className="text-xs px-2.5 py-1.5 rounded border border-slate-200 dark:border-slate-700 text-slate-500 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
            >
              Export CSV
            </button>
          )}
        </div>
      </div>

      {/* ── Loading / error ─────────────────────────────────────────────────── */}
      {isLoading && (
        <div className="flex items-center justify-center py-16 text-slate-400">
          <RefreshCw className="w-5 h-5 animate-spin mr-2" />
          Loading history…
        </div>
      )}

      {isError && (
        <div className="rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 px-4 py-3 text-sm text-red-600 dark:text-red-400">
          Failed to load quality history. Make sure the daemon is running and the group exists.
        </div>
      )}

      {!isLoading && !isError && entries.length === 0 && (
        <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 px-6 py-10 text-center">
          <Activity className="w-8 h-8 mx-auto mb-3 text-slate-300 dark:text-slate-600" />
          <p className="text-sm font-medium text-slate-600 dark:text-slate-400">
            No history yet
          </p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
            Run <code className="font-mono bg-slate-100 dark:bg-slate-800 px-1 rounded">archigraph rebuild {group}</code> a few times to start accumulating data.
          </p>
        </div>
      )}

      {!isLoading && !isError && entries.length > 0 && (
        <div className="space-y-6">
          {/* ── Trend chart ──────────────────────────────────────────────── */}
          <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4">
            <div className="flex items-center justify-between mb-3">
              <span className="text-sm font-medium text-slate-700 dark:text-slate-200">
                Health score over time
              </span>
              {latest && (
                <span className="font-mono text-sm text-slate-500 dark:text-slate-400">
                  current: <span className="font-semibold text-slate-800 dark:text-slate-100">{fmt1(latest.health_score)}</span>
                </span>
              )}
            </div>
            <TrendChart entries={entries} />
          </div>

          {/* ── Velocity comparison cards ─────────────────────────────────── */}
          {latest && (
            <div>
              <h2 className="text-xs font-semibold uppercase tracking-wide text-slate-400 dark:text-slate-500 mb-2">
                vs. earlier snapshots
              </h2>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
                <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 px-4 py-3 flex flex-col gap-1">
                  <div className="text-xs text-slate-500 dark:text-slate-400">Current</div>
                  <div className="font-mono text-2xl font-semibold text-slate-800 dark:text-slate-100">
                    {fmt1(latest.health_score)}
                  </div>
                  <div className="text-xs text-slate-400 dark:text-slate-500">
                    {fmtDateFull(latest.timestamp)}
                  </div>
                </div>
                <CompareCard
                  label="7 days ago"
                  current={latest.health_score}
                  reference={ref7}
                />
                <CompareCard
                  label="30 days ago"
                  current={latest.health_score}
                  reference={ref30}
                />
              </div>
            </div>
          )}

          {/* ── Current metric bars ───────────────────────────────────────── */}
          {latest && (
            <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-4 space-y-3">
              <h2 className="text-sm font-medium text-slate-700 dark:text-slate-200 mb-2">
                Latest snapshot metrics
              </h2>
              <MetricBar label="Orphan rate" value={latest.orphan_rate} danger={20} />
              <MetricBar label="Bug rate" value={latest.bug_rate} danger={10} />
              {latest.recall_pct !== undefined && (
                <MetricBar label="Recall" value={100 - latest.recall_pct} danger={20} />
              )}
              <div className="pt-1 text-xs text-slate-400 dark:text-slate-500">
                {latest.total_entities.toLocaleString()} total entities
              </div>
            </div>
          )}

          {/* ── Raw history table ─────────────────────────────────────────── */}
          <div className="rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden">
            <table className="w-full text-xs">
              <thead className="bg-slate-50 dark:bg-slate-800 text-slate-500 dark:text-slate-400">
                <tr>
                  <th className="text-left px-3 py-2 font-medium">Date</th>
                  <th className="text-right px-3 py-2 font-medium">Score</th>
                  <th className="text-right px-3 py-2 font-medium">Orphan %</th>
                  <th className="text-right px-3 py-2 font-medium">Bug %</th>
                  <th className="text-right px-3 py-2 font-medium">Entities</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {[...entries].reverse().map((e, i) => (
                  <tr
                    key={i}
                    className="hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
                  >
                    <td className="px-3 py-2 text-slate-600 dark:text-slate-400 font-mono">
                      {fmtDateFull(e.timestamp)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono font-semibold text-slate-800 dark:text-slate-100">
                      {fmt1(e.health_score)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-slate-500 dark:text-slate-400">
                      {fmt1(e.orphan_rate)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-slate-500 dark:text-slate-400">
                      {fmt1(e.bug_rate)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono text-slate-500 dark:text-slate-400">
                      {e.total_entities.toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Health score gauge
// ─────────────────────────────────────────────────────────────────────────────

function HealthScore({ score }: { score: number }) {
  const color = healthColor(score)
  const ringColor = score >= 80
    ? 'stroke-emerald-500'
    : score >= 60
    ? 'stroke-yellow-400'
    : 'stroke-red-500'

  const circumference = 2 * Math.PI * 36
  const offset = circumference * (1 - score / 100)

  return (
    <div className="flex flex-col items-center gap-1">
      <svg width="88" height="88" viewBox="0 0 88 88" className="-rotate-90">
        <circle cx="44" cy="44" r="36" fill="none" strokeWidth="8"
          className="stroke-slate-200 dark:stroke-slate-700" />
        <circle cx="44" cy="44" r="36" fill="none" strokeWidth="8"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          className={`transition-all duration-700 ${ringColor}`} />
      </svg>
      <div className="absolute mt-6">
        <span className={`text-2xl font-bold ${color}`}>{score}</span>
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">Health score</p>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Orphan tab
// ─────────────────────────────────────────────────────────────────────────────

function OrphanTab({ group }: { group: string }) {
  const [showAll, setShowAll] = useState(false)

  const {
    data,
    isFetching,
    refetch,
    isError,
    error,
  } = useQuery<OrphanAuditReply>({
    queryKey: ['quality-orphans', group],
    queryFn: () => fetchQualityOrphans(group),
    enabled: false, // manual run
    staleTime: 5 * 60 * 1000,
  })

  const run = useCallback(() => { refetch() }, [refetch])

  return (
    <div className="space-y-5">
      {/* Toolbar */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={run}
          disabled={isFetching}
          className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-sky-500 hover:bg-sky-600 disabled:opacity-50 text-white text-sm font-medium transition-colors"
        >
          {isFetching
            ? <RefreshCw className="w-4 h-4 animate-spin" />
            : <PlayCircle className="w-4 h-4" />
          }
          {isFetching ? 'Running…' : 'Run orphan audit'}
        </button>
        {data && (
          <button
            type="button"
            onClick={() => downloadJSON(data, `orphan-audit-${group}.json`)}
            className="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700 text-sm text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
          >
            <Download className="w-3.5 h-3.5" />
            Export JSON
          </button>
        )}
      </div>

      {isError && (
        <div className="flex items-center gap-2 p-3 rounded-lg bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 text-sm">
          <XCircle className="w-4 h-4 shrink-0" />
          {(error as Error)?.message ?? 'Audit failed'}
        </div>
      )}

      {!data && !isFetching && !isError && (
        <div className="flex flex-col items-center justify-center py-16 text-slate-400 dark:text-slate-600 gap-3">
          <ShieldCheck className="w-10 h-10" />
          <p className="text-sm">Click <strong>Run orphan audit</strong> to analyse the graph for unlinked entities.</p>
        </div>
      )}

      {data && (
        <div className="space-y-5">
          {/* Summary cards */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <SummaryCard label="Total entities" value={data.total.entities.toLocaleString()} />
            <SummaryCard label="Orphans" value={data.total.orphans.toLocaleString()} />
            <SummaryCard label="Orphan rate" value={pct(data.total.orphan_rate)}
              highlight={data.total.orphan_rate > 0.1 ? 'warn' : 'ok'} />
            <div className="relative flex items-center justify-center bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg p-3">
              <HealthScore score={data.health_score} />
            </div>
          </div>

          {/* Per-kind breakdown */}
          {data.per_kind.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2 flex items-center gap-1.5">
                <BarChart3 className="w-4 h-4" />
                Per-kind orphan rate
              </h3>
              <div className="space-y-1.5">
                {data.per_kind.slice(0, showAll ? undefined : 10).map((ks: KindStat) => (
                  <KindBar key={ks.kind} ks={ks} />
                ))}
                {data.per_kind.length > 10 && (
                  <button
                    type="button"
                    onClick={() => setShowAll(v => !v)}
                    className="text-xs text-sky-500 hover:underline"
                  >
                    {showAll ? 'Show less' : `Show all ${data.per_kind.length} kinds`}
                  </button>
                )}
              </div>
            </section>
          )}

          {/* Per-repo table */}
          {data.per_repo.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2">Per-repo breakdown</h3>
              <div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700">
                <table className="min-w-full text-sm">
                  <thead className="bg-slate-50 dark:bg-slate-800 text-slate-600 dark:text-slate-400 text-xs uppercase tracking-wide">
                    <tr>
                      <th className="px-4 py-2 text-left">Repo</th>
                      <th className="px-4 py-2 text-right">Entities</th>
                      <th className="px-4 py-2 text-right">Orphans</th>
                      <th className="px-4 py-2 text-right">Rate</th>
                      <th className="px-4 py-2 text-right">Risk</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {data.per_repo.map((r: RepoOrphanStats) => (
                      <tr key={r.slug} className="hover:bg-slate-50/50 dark:hover:bg-slate-800/50">
                        <td className="px-4 py-2 font-mono text-slate-700 dark:text-slate-300">{r.slug}</td>
                        <td className="px-4 py-2 text-right tabular-nums">{r.entities.toLocaleString()}</td>
                        <td className="px-4 py-2 text-right tabular-nums">{r.orphans.toLocaleString()}</td>
                        <td className="px-4 py-2 text-right tabular-nums">
                          <span className={r.orphan_rate > 0.1 ? 'text-red-600 dark:text-red-400 font-medium' : ''}>
                            {pct(r.orphan_rate)}
                          </span>
                        </td>
                        <td className="px-4 py-2 text-right tabular-nums">
                          <span className={healthColor(r.risk_score)}>{r.risk_score}</span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}

          {/* Recommendations */}
          {data.recommendations && data.recommendations.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2">Recommendations</h3>
              <ol className="space-y-2">
                {data.recommendations.map((rec) => (
                  <li key={rec.priority} className="flex gap-3 p-3 rounded-lg bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-700">
                    <AlertTriangle className="w-4 h-4 text-yellow-500 shrink-0 mt-0.5" />
                    <div className="text-sm text-yellow-800 dark:text-yellow-200">
                      <span className="font-medium">#{rec.priority}</span> {rec.issue}
                      <span className="ml-2 text-yellow-600 dark:text-yellow-400 text-xs">
                        ~{rec.recoverable_entities_estimate} recoverable entities across {rec.affected_repos} repo(s)
                      </span>
                    </div>
                  </li>
                ))}
              </ol>
            </section>
          )}
        </div>
      )}
    </div>
  )
}

function KindBar({ ks }: { ks: KindStat }) {
  const barColor = rateColor(ks.orphan_rate)
  const widthPct = Math.min(100, ks.orphan_rate * 100)

  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="w-40 truncate text-slate-600 dark:text-slate-400 font-mono text-xs" title={ks.kind}>
        {ks.kind}
      </span>
      <div className="flex-1 h-2 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
        <div className={`h-full rounded-full ${barColor}`} style={{ width: `${widthPct}%` }} />
      </div>
      <span className="w-14 text-right tabular-nums text-slate-500 dark:text-slate-400 text-xs">
        {pct(ks.orphan_rate)}
      </span>
      <span className="w-10 text-right tabular-nums text-slate-400 dark:text-slate-500 text-xs">
        {ks.count}
      </span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Recall tab
// ─────────────────────────────────────────────────────────────────────────────

function RecallTab() {
  const [selectedFixture, setSelectedFixture] = useState('')
  const [recallResult, setRecallResult] = useState<RecallReply | null>(null)
  const [showMissing, setShowMissing] = useState(false)

  const { data: fixturesData, isLoading: fixturesLoading } = useQuery({
    queryKey: ['quality-fixtures'],
    queryFn: fetchQualityFixtures,
    staleTime: 5 * 60 * 1000,
  })

  const { mutate: runRecall, isPending, isError, error } = useMutation({
    mutationFn: () => postQualityRecall(selectedFixture),
    onSuccess: (data) => {
      setRecallResult(data)
    },
  })

  const fixtures = fixturesData?.fixtures ?? []

  return (
    <div className="space-y-5">
      {/* Controls */}
      <div className="flex items-center gap-3 flex-wrap">
        <select
          value={selectedFixture}
          onChange={e => { setSelectedFixture(e.target.value); setRecallResult(null) }}
          disabled={fixturesLoading}
          className="px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-sm text-slate-700 dark:text-slate-300 focus:outline-none focus:ring-2 focus:ring-sky-500"
        >
          <option value="">
            {fixturesLoading ? 'Loading fixtures…' : 'Select fixture…'}
          </option>
          {fixtures.map(f => (
            <option key={f} value={f}>{f}</option>
          ))}
        </select>

        <button
          type="button"
          onClick={() => runRecall()}
          disabled={!selectedFixture || isPending}
          className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-sky-500 hover:bg-sky-600 disabled:opacity-50 text-white text-sm font-medium transition-colors"
        >
          {isPending
            ? <RefreshCw className="w-4 h-4 animate-spin" />
            : <PlayCircle className="w-4 h-4" />
          }
          {isPending ? 'Running…' : 'Run recall'}
        </button>

        {recallResult && (
          <button
            type="button"
            onClick={() => downloadJSON(recallResult, `recall-${selectedFixture}.json`)}
            className="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700 text-sm text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
          >
            <Download className="w-3.5 h-3.5" />
            Export JSON
          </button>
        )}
      </div>

      {isError && (
        <div className="flex items-center gap-2 p-3 rounded-lg bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 text-sm">
          <XCircle className="w-4 h-4 shrink-0" />
          {(error as Error)?.message ?? 'Recall measurement failed'}
        </div>
      )}

      {!recallResult && !isPending && !isError && (
        <div className="flex flex-col items-center justify-center py-16 text-slate-400 dark:text-slate-600 gap-3">
          <ShieldAlert className="w-10 h-10" />
          <p className="text-sm">Select a fixture and click <strong>Run recall</strong> to measure extraction quality.</p>
        </div>
      )}

      {recallResult && (
        <div className="space-y-5">
          {/* Score cards */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <RecallScoreCard
              label="Entity recall"
              value={recallResult.entity_recall}
              found={recallResult.entity_found}
              expected={recallResult.entity_expected}
            />
            <RecallScoreCard
              label="Relationship recall"
              value={recallResult.relationship_recall}
              found={recallResult.relationship_found}
              expected={recallResult.relationship_expected}
            />
            <SummaryCard
              label="Forbidden hits"
              value={String(recallResult.forbidden_hits)}
              highlight={recallResult.forbidden_hits > 0 ? 'error' : 'ok'}
            />
            <SummaryCard
              label="Fixture"
              value={recallResult.fixture}
            />
          </div>

          {/* Missing entities */}
          {(recallResult.missing_entities?.length ?? 0) > 0 && (
            <section>
              <button
                type="button"
                className="flex items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2"
                onClick={() => setShowMissing(v => !v)}
              >
                <XCircle className="w-4 h-4 text-red-400" />
                Missing entities ({recallResult.missing_entities!.length})
                {showMissing ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
              </button>
              {showMissing && (
                <div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700">
                  <table className="min-w-full text-sm">
                    <thead className="bg-slate-50 dark:bg-slate-800 text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">
                      <tr>
                        <th className="px-4 py-2 text-left">Name</th>
                        <th className="px-4 py-2 text-left">Kind</th>
                        <th className="px-4 py-2 text-left">File</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {recallResult.missing_entities!.map((e, i) => (
                        <tr key={i} className="hover:bg-slate-50/50 dark:hover:bg-slate-800/50">
                          <td className="px-4 py-1.5 font-mono text-xs text-slate-700 dark:text-slate-300">{e.name}</td>
                          <td className="px-4 py-1.5 text-xs text-slate-500 dark:text-slate-400">{e.kind}</td>
                          <td className="px-4 py-1.5 font-mono text-xs text-slate-500 dark:text-slate-400 truncate max-w-xs">{e.source_file ?? '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          )}

          {/* Missing relationships */}
          {(recallResult.missing_relationships?.length ?? 0) > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2 flex items-center gap-1.5">
                <AlertTriangle className="w-4 h-4 text-yellow-400" />
                Missing relationships ({recallResult.missing_relationships!.length})
              </h3>
              <div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700">
                <table className="min-w-full text-sm">
                  <thead className="bg-slate-50 dark:bg-slate-800 text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">
                    <tr>
                      <th className="px-4 py-2 text-left">From</th>
                      <th className="px-4 py-2 text-left">Edge</th>
                      <th className="px-4 py-2 text-left">To</th>
                      <th className="px-4 py-2 text-left">Root cause</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {recallResult.missing_relationships!.map((rel, i) => {
                      const rootCause = !rel.from_resolved && !rel.to_resolved
                        ? 'Neither endpoint extracted'
                        : !rel.from_resolved
                        ? 'From-entity not extracted'
                        : !rel.to_resolved
                        ? 'To-entity not extracted'
                        : 'Both endpoints exist; edge not emitted'
                      return (
                        <tr key={i} className="hover:bg-slate-50/50 dark:hover:bg-slate-800/50">
                          <td className="px-4 py-1.5 font-mono text-xs text-slate-700 dark:text-slate-300">{rel.from}</td>
                          <td className="px-4 py-1.5 text-xs text-slate-500">
                            <span className="px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 font-mono">{rel.kind}</span>
                          </td>
                          <td className="px-4 py-1.5 font-mono text-xs text-slate-700 dark:text-slate-300">{rel.to}</td>
                          <td className="px-4 py-1.5 text-xs text-slate-500 dark:text-slate-400">{rootCause}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </section>
          )}

          {/* All good */}
          {(recallResult.missing_entities?.length ?? 0) === 0 &&
           (recallResult.missing_relationships?.length ?? 0) === 0 &&
           recallResult.forbidden_hits === 0 && (
            <div className="flex items-center gap-2 p-4 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-700">
              <CheckCircle2 className="w-5 h-5 text-emerald-500" />
              <span className="text-sm text-emerald-700 dark:text-emerald-300 font-medium">
                Perfect recall — all expected entities and relationships found, no forbidden hits.
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function RecallScoreCard({
  label, value, found, expected,
}: {
  label: string
  value: number
  found: number
  expected: number
}) {
  const pctStr = pct(value)
  const color = value >= 0.9
    ? 'text-emerald-600 dark:text-emerald-400'
    : value >= 0.7
    ? 'text-yellow-600 dark:text-yellow-400'
    : 'text-red-600 dark:text-red-400'

  return (
    <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg p-3 flex flex-col gap-0.5">
      <span className="text-xs text-slate-500 dark:text-slate-400">{label}</span>
      <span className={`text-xl font-bold ${color}`}>{pctStr}</span>
      <span className="text-xs text-slate-400 dark:text-slate-500">{found} / {expected}</span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared summary card
// ─────────────────────────────────────────────────────────────────────────────

function SummaryCard({
  label,
  value,
  highlight,
}: {
  label: string
  value: string
  highlight?: 'ok' | 'warn' | 'error'
}) {
  const valColor = highlight === 'error'
    ? 'text-red-600 dark:text-red-400'
    : highlight === 'warn'
    ? 'text-yellow-600 dark:text-yellow-400'
    : highlight === 'ok'
    ? 'text-emerald-600 dark:text-emerald-400'
    : 'text-slate-800 dark:text-slate-200'

  return (
    <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg p-3 flex flex-col gap-0.5">
      <span className="text-xs text-slate-500 dark:text-slate-400">{label}</span>
      <span className={`text-xl font-bold truncate ${valColor}`}>{value}</span>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Root route component
// ─────────────────────────────────────────────────────────────────────────────

export function QualityRoute() {
  const { group = 'fixture-a' } = useParams()
  const [activeTab, setActiveTab] = useState<Tab>('history')

  return (
    <div className="h-full overflow-y-auto bg-slate-50 dark:bg-slate-950">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-5">
        {/* Page header */}
        <div className="flex items-center gap-3">
          <ShieldCheck className="w-6 h-6 text-sky-500" />
          <div>
            <h1 className="text-lg font-bold text-slate-800 dark:text-slate-200">Quality</h1>
            <p className="text-sm text-slate-500 dark:text-slate-400">
              Health-score history, orphan audit, and recall measurement for <strong>{group}</strong>
            </p>
          </div>
        </div>

        {/* Tabs */}
        <div className="flex gap-1 border-b border-slate-200 dark:border-slate-800">
          {(['history', 'orphans', 'recall'] as const).map(tab => (
            <button
              key={tab}
              type="button"
              onClick={() => setActiveTab(tab)}
              className={[
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors',
                activeTab === tab
                  ? 'border-sky-500 text-sky-600 dark:text-sky-400'
                  : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300',
              ].join(' ')}
            >
              {tab === 'history' ? 'History' : tab === 'orphans' ? 'Orphan audit' : 'Recall measurement'}
            </button>
          ))}
        </div>

        {/* Tab content */}
        <div className="min-h-64">
          {activeTab === 'history' && <HistoryTab group={group} />}
          {activeTab === 'orphans' && <OrphanTab group={group} />}
          {activeTab === 'recall' && <RecallTab />}
        </div>
      </div>
    </div>
  )
}
