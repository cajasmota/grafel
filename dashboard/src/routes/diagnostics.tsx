import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  fetchDiagnostics, postKillStale,
  type DiagnosticsReply, type GroupDiagnostics, type RepoDiagnostics,
  type IssueDiagnostic, type KilledProcess,
} from '@/api/client'
import {
  CheckCircle2, AlertTriangle, XCircle, RefreshCw,
  Cpu, HardDrive, Wifi, WifiOff, ShieldCheck, ShieldOff,
  Server, Layers, ChevronDown, ChevronRight, Trash2,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Auto-refresh hook
// ─────────────────────────────────────────────────────────────────────────────

const REFRESH_INTERVAL_MS = 30_000

// ─────────────────────────────────────────────────────────────────────────────
// Health badge
// ─────────────────────────────────────────────────────────────────────────────

interface HealthBadgeProps {
  status: 'HEALTHY' | 'DEGRADED' | 'FAILED' | 'OK' | 'STALE' | 'MISSING' | string
  size?: 'sm' | 'md'
}

function HealthBadge({ status, size = 'md' }: HealthBadgeProps) {
  const base = size === 'sm'
    ? 'inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold border'
    : 'inline-flex items-center gap-1.5 px-2 py-1 rounded text-xs font-semibold border'

  const variants: Record<string, string> = {
    HEALTHY: 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 border-emerald-200 dark:border-emerald-700',
    OK:      'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 border-emerald-200 dark:border-emerald-700',
    DEGRADED:'bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
    STALE:   'bg-yellow-50 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
    FAILED:  'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
    MISSING: 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
  }

  const icons: Record<string, React.ReactNode> = {
    HEALTHY:  <CheckCircle2 className="w-3 h-3" />,
    OK:       <CheckCircle2 className="w-3 h-3" />,
    DEGRADED: <AlertTriangle className="w-3 h-3" />,
    STALE:    <AlertTriangle className="w-3 h-3" />,
    FAILED:   <XCircle className="w-3 h-3" />,
    MISSING:  <XCircle className="w-3 h-3" />,
  }

  const cls = variants[status] ?? 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700'

  return (
    <span className={`${base} ${cls}`}>
      {icons[status] ?? null}
      {status}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Check row — one daemon infrastructure check
// ─────────────────────────────────────────────────────────────────────────────

interface CheckRowProps {
  label: string
  ok: boolean
  value?: string
  hint?: string
}

function CheckRow({ label, ok, value, hint }: CheckRowProps) {
  return (
    <div className="flex items-center gap-3 py-1.5 text-sm border-b border-slate-100 dark:border-slate-800 last:border-0">
      {ok
        ? <CheckCircle2 className="w-4 h-4 text-emerald-500 shrink-0" />
        : <XCircle className="w-4 h-4 text-red-400 shrink-0" />
      }
      <span className="text-slate-600 dark:text-slate-400 w-48 shrink-0">{label}</span>
      {value && (
        <span className="text-slate-800 dark:text-slate-200 font-mono text-xs truncate" title={hint ?? value}>
          {value}
        </span>
      )}
      {!value && hint && (
        <span className="text-slate-400 dark:text-slate-500 text-xs italic">{hint}</span>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Daemon panel
// ─────────────────────────────────────────────────────────────────────────────

function DaemonPanel({ data }: { data: DiagnosticsReply }) {
  const d = data.daemon
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50">
        <div className="flex items-center gap-2">
          <Server className="w-4 h-4 text-slate-500" />
          <span className="font-semibold text-sm text-slate-800 dark:text-slate-200">Daemon</span>
          <HealthBadge status={d.running ? 'HEALTHY' : 'FAILED'} />
        </div>
        <div className="flex items-center gap-4 text-xs text-slate-500 dark:text-slate-400">
          {d.uptime_human && <span>Up {d.uptime_human}</span>}
          {d.pid > 0 && <span className="font-mono">PID {d.pid}</span>}
          <span className="font-mono">{d.rss_mb.toFixed(1)} MB RSS</span>
        </div>
      </div>

      {/* Checks grid */}
      <div className="px-4 py-2 grid grid-cols-1 md:grid-cols-2 gap-x-8">
        <div>
          <CheckRow label="Socket reachable" ok={d.socket_reachable} />
          <CheckRow label="Workspace writable" ok={d.workspace_writable} />
          <CheckRow label="Dashboard port" ok={d.dashboard_port_available} value={d.dashboard_port > 0 ? String(d.dashboard_port) : undefined} />
          <CheckRow label="LaunchAgent installed" ok={d.launch_agent_installed} hint={!d.launch_agent_installed ? 'macOS only — run archigraph install' : undefined} />
        </div>
        <div>
          <CheckRow label="MCP: Claude Code" ok={d.mcp_claude_code} />
          <CheckRow label="MCP: Windsurf" ok={d.mcp_windsurf} />
          <CheckRow label="Registry" ok={!!d.registry_path} value={d.registry_path ? `${d.group_count} group(s)` : undefined} />
        </div>
      </div>

      {/* Build info footer */}
      <div className="px-4 py-2 border-t border-slate-100 dark:border-slate-800 flex items-center gap-4 text-xs text-slate-400 dark:text-slate-500">
        <span>v{d.version}</span>
        {d.commit && (
          <a
            href={`https://github.com/cajasmota/archigraph/commit/${d.commit}`}
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono hover:text-sky-400 transition-colors"
          >
            {d.commit.slice(0, 7)}
          </a>
        )}
        {d.built_at && <span>built {d.built_at}</span>}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Repo row
// ─────────────────────────────────────────────────────────────────────────────

function RepoRow({ repo }: { repo: RepoDiagnostics }) {
  return (
    <div className="flex items-center gap-3 px-4 py-2 border-b border-slate-100 dark:border-slate-800 last:border-0 text-xs hover:bg-slate-50/60 dark:hover:bg-slate-800/30 transition-colors">
      <HealthBadge status={repo.status} size="sm" />
      <span className="font-mono text-slate-700 dark:text-slate-300 w-40 truncate shrink-0" title={repo.path}>
        {repo.slug}
      </span>
      <span className="text-slate-400 dark:text-slate-500 shrink-0">indexed {repo.last_indexed_age}</span>
      <span className="text-slate-500 dark:text-slate-400 shrink-0">{repo.entities.toLocaleString()} entities</span>
      <span className="text-slate-500 dark:text-slate-400 shrink-0">{repo.relationships.toLocaleString()} rels</span>
      {repo.cross_repo_edges > 0 && (
        <span className="text-slate-400 dark:text-slate-500 shrink-0">{repo.cross_repo_edges} cross-repo</span>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Issue item
// ─────────────────────────────────────────────────────────────────────────────

function IssueItem({ issue }: { issue: IssueDiagnostic }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="border-b border-slate-100 dark:border-slate-800 last:border-0">
      <button
        type="button"
        className="w-full flex items-start gap-2 px-4 py-2 text-left hover:bg-slate-50/60 dark:hover:bg-slate-800/30 transition-colors"
        onClick={() => setExpanded(v => !v)}
      >
        <AlertTriangle className="w-3.5 h-3.5 text-yellow-500 shrink-0 mt-0.5" />
        <span className="flex-1 text-xs text-slate-700 dark:text-slate-300">{issue.description}</span>
        {issue.remediation && (
          expanded
            ? <ChevronDown className="w-3.5 h-3.5 text-slate-400 shrink-0" />
            : <ChevronRight className="w-3.5 h-3.5 text-slate-400 shrink-0" />
        )}
      </button>
      {expanded && issue.remediation && (
        <div className="px-4 pb-2 pl-9 text-xs text-slate-500 dark:text-slate-400 italic">
          {issue.remediation}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Group card
// ─────────────────────────────────────────────────────────────────────────────

function GroupCard({ group }: { group: GroupDiagnostics }) {
  const [reposOpen, setReposOpen] = useState(false)

  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-slate-500" />
          <span className="font-semibold text-sm text-slate-800 dark:text-slate-200">{group.name}</span>
          <HealthBadge status={group.status} />
        </div>
        <div className="flex items-center gap-4 text-xs text-slate-500 dark:text-slate-400">
          <span>{group.total_entities.toLocaleString()} entities</span>
          {group.orphan_rate > 0 && (
            <span className={group.orphan_rate > 20 ? 'text-yellow-500' : ''}>
              {group.orphan_rate.toFixed(1)}% orphan
            </span>
          )}
          {group.pending_repairs > 0 && (
            <span className="text-orange-500">{group.pending_repairs} repairs</span>
          )}
          {group.pending_enrichments > 0 && (
            <span className="text-sky-500">{group.pending_enrichments} enrichments</span>
          )}
        </div>
      </div>

      {/* Issues found */}
      {group.issues_found.length > 0 && (
        <div className="border-b border-slate-200 dark:border-slate-700">
          {group.issues_found.map((issue, i) => (
            <IssueItem key={i} issue={issue} />
          ))}
        </div>
      )}

      {/* Repo list toggle */}
      {group.repos.length > 0 && (
        <>
          <button
            type="button"
            className="w-full flex items-center justify-between px-4 py-2 text-xs text-slate-500 dark:text-slate-400 hover:bg-slate-50/60 dark:hover:bg-slate-800/30 transition-colors"
            onClick={() => setReposOpen(v => !v)}
          >
            <span>{group.repos.length} repo{group.repos.length !== 1 ? 's' : ''}</span>
            {reposOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          </button>
          {reposOpen && (
            <div>
              {group.repos.map(r => <RepoRow key={r.slug} repo={r} />)}
            </div>
          )}
        </>
      )}

      {/* Quality metrics footer */}
      <div className="px-4 py-2 border-t border-slate-100 dark:border-slate-800 flex items-center gap-4 text-xs text-slate-400 dark:text-slate-500">
        <span>Bug-rate {group.bug_rate.toFixed(1)}%</span>
        <span>{group.orphan_entities} orphan entities</span>
        {group.watcher_events_dropped > 0 && (
          <span className="text-yellow-500">{group.watcher_events_dropped} events dropped</span>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Kill-stale confirm modal
// ─────────────────────────────────────────────────────────────────────────────

interface KillStaleModalProps {
  onConfirm: () => void
  onCancel: () => void
  isLoading: boolean
  result?: KilledProcess[]
}

function KillStaleModal({ onConfirm, onCancel, isLoading, result }: KillStaleModalProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-2xl p-6 w-full max-w-md">
        <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-2">Kill stale daemons?</h2>
        {result == null ? (
          <>
            <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
              This will send SIGTERM to any stale archigraph daemon processes (orphaned or running from a different binary).
              Your current dashboard will continue running.
            </p>
            <div className="flex items-center gap-3 justify-end">
              <button
                type="button"
                className="px-3 py-1.5 text-sm rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
                onClick={onCancel}
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={isLoading}
                className="px-3 py-1.5 text-sm rounded bg-red-600 text-white font-medium hover:bg-red-700 disabled:opacity-50 transition-colors flex items-center gap-1.5"
                onClick={onConfirm}
              >
                {isLoading && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
                Kill stale daemons
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="text-sm text-slate-600 dark:text-slate-400 mb-4">
              {result.length === 0 ? (
                <span className="flex items-center gap-2 text-emerald-600 dark:text-emerald-400">
                  <CheckCircle2 className="w-4 h-4" />
                  No stale daemons found.
                </span>
              ) : (
                <ul className="space-y-1">
                  {result.map(p => (
                    <li key={p.pid} className="flex items-center gap-2 font-mono text-xs">
                      {p.killed
                        ? <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
                        : <XCircle className="w-3.5 h-3.5 text-red-400" />
                      }
                      pid {p.pid} — {p.exe.split('/').pop()}
                      {p.kill_err && <span className="text-red-400"> ({p.kill_err})</span>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div className="flex justify-end">
              <button
                type="button"
                className="px-3 py-1.5 text-sm rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
                onClick={onCancel}
              >
                Close
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Skeleton
// ─────────────────────────────────────────────────────────────────────────────

function DiagnosticsSkeleton() {
  return (
    <div className="space-y-4 animate-pulse">
      {[1, 2, 3].map(i => (
        <div key={i} className="rounded-lg border border-slate-200 dark:border-slate-700 h-24 bg-slate-100 dark:bg-slate-800" />
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main route
// ─────────────────────────────────────────────────────────────────────────────

export function DiagnosticsRoute() {
  const qc = useQueryClient()
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [killStaleOpen, setKillStaleOpen] = useState(false)
  const [killResult, setKillResult] = useState<KilledProcess[] | undefined>()

  const {
    data,
    isLoading,
    isFetching,
    refetch,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ['diagnostics'],
    queryFn: fetchDiagnostics,
    refetchInterval: autoRefresh ? REFRESH_INTERVAL_MS : false,
    staleTime: 10_000,
  })

  const killMutation = useMutation({
    mutationFn: () => postKillStale(false),
    onSuccess: (res) => {
      setKillResult(res.killed)
      void qc.invalidateQueries({ queryKey: ['diagnostics'] })
    },
  })

  const handleRefresh = useCallback(() => { void refetch() }, [refetch])

  const checkedAtLabel = dataUpdatedAt
    ? new Date(dataUpdatedAt).toLocaleTimeString()
    : null

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-4xl mx-auto px-4 py-6 space-y-6">
        {/* Toolbar */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">Diagnostics</h1>
            {data?.nominal && (
              <span className="inline-flex items-center gap-1.5 text-xs text-emerald-600 dark:text-emerald-400 font-medium">
                <CheckCircle2 className="w-4 h-4" />
                All systems nominal
              </span>
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* Auto-refresh toggle */}
            <label className="flex items-center gap-1.5 text-xs text-slate-500 dark:text-slate-400 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={autoRefresh}
                onChange={e => setAutoRefresh(e.target.checked)}
                className="w-3 h-3 accent-sky-500"
              />
              Auto-refresh 30s
            </label>

            {checkedAtLabel && (
              <span className="text-xs text-slate-400 dark:text-slate-500">
                Last check {checkedAtLabel}
              </span>
            )}

            <button
              type="button"
              onClick={handleRefresh}
              disabled={isFetching}
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-50 transition-colors"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
              Run health check
            </button>

            <button
              type="button"
              onClick={() => { setKillResult(undefined); setKillStaleOpen(true) }}
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
            >
              <Trash2 className="w-3.5 h-3.5" />
              Kill stale daemons
            </button>
          </div>
        </div>

        {/* Body */}
        {isLoading && <DiagnosticsSkeleton />}

        {data && (
          <>
            {/* Daemon panel */}
            <DaemonPanel data={data} />

            {/* Connectivity summary row */}
            <div className="flex items-center gap-4 px-1 flex-wrap">
              <StatusChip
                icon={data.daemon.socket_reachable ? <Wifi className="w-3.5 h-3.5" /> : <WifiOff className="w-3.5 h-3.5" />}
                label="Socket"
                ok={data.daemon.socket_reachable}
              />
              <StatusChip
                icon={data.daemon.mcp_claude_code ? <ShieldCheck className="w-3.5 h-3.5" /> : <ShieldOff className="w-3.5 h-3.5" />}
                label="MCP Claude Code"
                ok={data.daemon.mcp_claude_code}
              />
              <StatusChip
                icon={data.daemon.mcp_windsurf ? <ShieldCheck className="w-3.5 h-3.5" /> : <ShieldOff className="w-3.5 h-3.5" />}
                label="MCP Windsurf"
                ok={data.daemon.mcp_windsurf}
              />
              <StatusChip
                icon={<Cpu className="w-3.5 h-3.5" />}
                label={`${data.daemon.rss_mb.toFixed(0)} MB RSS`}
                ok={data.daemon.rss_mb < 800}
              />
              <StatusChip
                icon={<HardDrive className="w-3.5 h-3.5" />}
                label="Workspace writable"
                ok={data.daemon.workspace_writable}
              />
            </div>

            {/* Empty state when all nominal */}
            {data.nominal && data.groups.length === 0 && (
              <div className="flex flex-col items-center justify-center py-16 text-slate-400 dark:text-slate-500 gap-3">
                <CheckCircle2 className="w-10 h-10 text-emerald-400" />
                <p className="text-sm font-medium">All systems nominal</p>
                <p className="text-xs">No groups registered yet. Run <code className="font-mono bg-slate-100 dark:bg-slate-800 px-1 rounded">archigraph onboard</code> to get started.</p>
              </div>
            )}

            {/* Per-group cards */}
            {data.groups.length > 0 && (
              <section className="space-y-3">
                <h2 className="text-sm font-medium text-slate-700 dark:text-slate-300">Groups ({data.groups.length})</h2>
                {data.groups.map(g => <GroupCard key={g.name} group={g} />)}
              </section>
            )}
          </>
        )}
      </div>

      {/* Kill-stale modal */}
      {killStaleOpen && (
        <KillStaleModal
          onConfirm={() => killMutation.mutate()}
          onCancel={() => { setKillStaleOpen(false); setKillResult(undefined) }}
          isLoading={killMutation.isPending}
          result={killResult}
        />
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// StatusChip — small inline ok/warn pill
// ─────────────────────────────────────────────────────────────────────────────

interface StatusChipProps {
  icon: React.ReactNode
  label: string
  ok: boolean
}

function StatusChip({ icon, label, ok }: StatusChipProps) {
  return (
    <span className={[
      'inline-flex items-center gap-1 px-2 py-1 rounded-full text-[11px] border',
      ok
        ? 'bg-emerald-50 dark:bg-emerald-900/20 text-emerald-600 dark:text-emerald-400 border-emerald-100 dark:border-emerald-800'
        : 'bg-red-50 dark:bg-red-900/20 text-red-600 dark:text-red-400 border-red-100 dark:border-red-800',
    ].join(' ')}>
      {icon}
      {label}
    </span>
  )
}
