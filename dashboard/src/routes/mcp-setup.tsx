/**
 * MCP Setup Wizard — Surface 13 (issue #1247)
 *
 * Detects which MCP hosts are installed (Claude Code, Cursor, Windsurf),
 * shows the current config state, and allows one-click install / uninstall
 * / verify without manual copy-paste.
 */

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Zap, CheckCircle, AlertTriangle, XCircle, RefreshCw,
  Download, Trash2, Activity, ChevronDown, ChevronRight,
  FileText,
} from 'lucide-react'
import {
  fetchMCPHosts,
  installMCPHost,
  uninstallMCPHost,
  verifyMCPHost,
  type MCPHostInfo,
  type MCPInstallState,
  type MCPHostsReply,
} from '@/api/mcp-setup'

// ── Helpers ───────────────────────────────────────────────────────────────────

function cls(...parts: (string | boolean | undefined)[]) {
  return parts.filter(Boolean).join(' ')
}

// ── State chip ────────────────────────────────────────────────────────────────

interface StateChipProps {
  state: MCPInstallState
}

const STATE_META: Record<
  MCPInstallState,
  { label: string; icon: React.ReactNode; className: string }
> = {
  installed: {
    label: 'Installed',
    icon: <CheckCircle className="w-3.5 h-3.5" />,
    className:
      'bg-emerald-50 dark:bg-emerald-950/40 text-emerald-700 dark:text-emerald-400 border-emerald-200 dark:border-emerald-800',
  },
  partial: {
    label: 'Partial',
    icon: <AlertTriangle className="w-3.5 h-3.5" />,
    className:
      'bg-amber-50 dark:bg-amber-950/40 text-amber-700 dark:text-amber-400 border-amber-200 dark:border-amber-800',
  },
  not_installed: {
    label: 'Not installed',
    icon: <XCircle className="w-3.5 h-3.5" />,
    className:
      'bg-slate-50 dark:bg-slate-900 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700',
  },
  host_absent: {
    label: 'Host not found',
    icon: <XCircle className="w-3.5 h-3.5" />,
    className:
      'bg-slate-50 dark:bg-slate-900 text-slate-400 dark:text-slate-500 border-slate-200 dark:border-slate-700',
  },
}

function StateChip({ state }: StateChipProps) {
  const { label, icon, className } = STATE_META[state]
  return (
    <span
      className={cls(
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border',
        className,
      )}
    >
      {icon}
      {label}
    </span>
  )
}

// ── Result banner ─────────────────────────────────────────────────────────────

interface ResultBannerProps {
  ok: boolean
  message: string
  latencyMs?: number
  onDismiss: () => void
}

function ResultBanner({ ok, message, latencyMs, onDismiss }: ResultBannerProps) {
  return (
    <div
      className={cls(
        'flex items-start gap-2 px-3 py-2 rounded text-xs border',
        ok
          ? 'bg-emerald-50 dark:bg-emerald-950/30 border-emerald-200 dark:border-emerald-800 text-emerald-700 dark:text-emerald-400'
          : 'bg-red-50 dark:bg-red-950/30 border-red-200 dark:border-red-800 text-red-700 dark:text-red-400',
      )}
    >
      {ok ? (
        <CheckCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
      ) : (
        <XCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
      )}
      <span className="flex-1">
        {message}
        {latencyMs !== undefined && (
          <span className="ml-1 opacity-60">({latencyMs}ms)</span>
        )}
      </span>
      <button
        type="button"
        onClick={onDismiss}
        className="opacity-50 hover:opacity-100 transition-opacity flex-shrink-0"
        aria-label="Dismiss"
      >
        ×
      </button>
    </div>
  )
}

// ── Config path display ───────────────────────────────────────────────────────

interface ConfigPathRowProps {
  path: string
  exists: boolean
  currentArgs?: string[]
}

function ConfigPathRow({ path, exists, currentArgs }: ConfigPathRowProps) {
  const [open, setOpen] = useState(false)
  return (
    <div className="space-y-1">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
      >
        {open ? (
          <ChevronDown className="w-3 h-3" />
        ) : (
          <ChevronRight className="w-3 h-3" />
        )}
        <FileText className="w-3 h-3" />
        <span className="font-mono truncate max-w-xs">{path}</span>
        {!exists && (
          <span className="text-slate-300 dark:text-slate-600">(not found)</span>
        )}
      </button>
      {open && (
        <div className="ml-5 text-xs text-slate-400 dark:text-slate-500 space-y-0.5">
          <p>
            <strong>Status:</strong> {exists ? 'file exists' : 'file not found'}
          </p>
          {currentArgs && currentArgs.length > 0 && (
            <p>
              <strong>Current args:</strong>{' '}
              <code className="font-mono">{currentArgs.join(' ')}</code>
            </p>
          )}
        </div>
      )}
    </div>
  )
}

// ── Host card ─────────────────────────────────────────────────────────────────

interface HostCardProps {
  host: MCPHostInfo
  onRefresh: () => void
}

interface ActionResult {
  ok: boolean
  message: string
  latencyMs?: number
}

function HostCard({ host, onRefresh }: HostCardProps) {
  const [result, setResult] = useState<ActionResult | null>(null)

  const installMutation = useMutation({
    mutationFn: () => installMCPHost(host.id),
    onSuccess: (r) => {
      setResult({ ok: r.ok, message: r.message })
      onRefresh()
    },
    onError: (e: Error) => setResult({ ok: false, message: e.message }),
  })

  const uninstallMutation = useMutation({
    mutationFn: () => uninstallMCPHost(host.id),
    onSuccess: (r) => {
      setResult({ ok: r.ok, message: r.message })
      onRefresh()
    },
    onError: (e: Error) => setResult({ ok: false, message: e.message }),
  })

  const verifyMutation = useMutation({
    mutationFn: () => verifyMCPHost(host.id),
    onSuccess: (r) =>
      setResult({ ok: r.ok, message: r.message, latencyMs: r.latency_ms }),
    onError: (e: Error) => setResult({ ok: false, message: e.message }),
  })

  const busy =
    installMutation.isPending ||
    uninstallMutation.isPending ||
    verifyMutation.isPending

  const isInstalled = host.state === 'installed'
  const isAbsent = host.state === 'host_absent'

  return (
    <div className="border border-slate-200 dark:border-slate-800 rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 bg-slate-50 dark:bg-slate-900">
        <Zap className="w-4 h-4 text-sky-400 flex-shrink-0" />
        <span className="flex-1 font-medium text-sm text-slate-800 dark:text-slate-200">
          {host.label}
        </span>
        <StateChip state={host.state} />
      </div>

      {/* Body */}
      <div className="px-4 py-3 bg-white dark:bg-slate-950 space-y-3">
        {/* Config path */}
        {host.config_path && (
          <ConfigPathRow
            path={host.config_path}
            exists={host.exists}
            currentArgs={host.current_args}
          />
        )}

        {/* Error from detection */}
        {host.error && (
          <p className="text-xs text-amber-500 dark:text-amber-400">
            Detection note: {host.error}
          </p>
        )}

        {/* Action result */}
        {result && (
          <ResultBanner
            ok={result.ok}
            message={result.message}
            latencyMs={result.latencyMs}
            onDismiss={() => setResult(null)}
          />
        )}

        {/* Buttons */}
        {!isAbsent && (
          <div className="flex items-center gap-2 flex-wrap">
            {!isInstalled ? (
              <button
                type="button"
                disabled={busy}
                onClick={() => installMutation.mutate()}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
                  bg-sky-500 text-white hover:bg-sky-600 disabled:opacity-40
                  disabled:cursor-not-allowed transition-colors"
              >
                {installMutation.isPending ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Download className="w-3.5 h-3.5" />
                )}
                Install
              </button>
            ) : (
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  if (
                    window.confirm(
                      `Remove archigraph from ${host.label}'s MCP config?`,
                    )
                  ) {
                    uninstallMutation.mutate()
                  }
                }}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
                  border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400
                  hover:bg-red-50 dark:hover:bg-red-950/30 disabled:opacity-40
                  disabled:cursor-not-allowed transition-colors"
              >
                {uninstallMutation.isPending ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Trash2 className="w-3.5 h-3.5" />
                )}
                Uninstall
              </button>
            )}

            <button
              type="button"
              disabled={busy}
              onClick={() => verifyMutation.mutate()}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
                border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
                hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40
                disabled:cursor-not-allowed transition-colors"
            >
              {verifyMutation.isPending ? (
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Activity className="w-3.5 h-3.5" />
              )}
              Verify
            </button>
          </div>
        )}

        {/* Reinstall option for partial state */}
        {host.state === 'partial' && (
          <p className="text-xs text-amber-500 dark:text-amber-400">
            The archigraph entry exists but may be misconfigured. Click Install to
            overwrite with the correct settings.
          </p>
        )}
      </div>
    </div>
  )
}

// ── Welcome banner (no hosts detected) ───────────────────────────────────────

function NoHostsBanner() {
  return (
    <div className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900 px-5 py-6 text-center space-y-2">
      <Zap className="w-8 h-8 text-sky-300 mx-auto" />
      <p className="text-sm font-medium text-slate-700 dark:text-slate-300">
        No MCP hosts found
      </p>
      <p className="text-xs text-slate-500 dark:text-slate-500 max-w-sm mx-auto">
        Install{' '}
        <a
          href="https://claude.ai/download"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-700 dark:hover:text-slate-300"
        >
          Claude Code
        </a>
        ,{' '}
        <a
          href="https://cursor.com"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-700 dark:hover:text-slate-300"
        >
          Cursor
        </a>
        , or{' '}
        <a
          href="https://codeium.com/windsurf"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-700 dark:hover:text-slate-300"
        >
          Windsurf
        </a>{' '}
        to use archigraph as an MCP server.
      </p>
    </div>
  )
}

// ── Main route ────────────────────────────────────────────────────────────────

const QUERY_KEY = ['mcp-setup', 'hosts'] as const

export function MCPSetupRoute() {
  const qc = useQueryClient()

  const { data, isLoading, error, refetch } = useQuery<MCPHostsReply>({
    queryKey: QUERY_KEY,
    queryFn: fetchMCPHosts,
    staleTime: 10_000,
  })

  const refresh = () => {
    qc.invalidateQueries({ queryKey: QUERY_KEY })
    void refetch()
  }

  const allAbsent =
    data?.hosts.every((h) => h.state === 'host_absent') ?? false

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-2xl mx-auto px-4 py-8 space-y-5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Zap className="w-6 h-6 text-sky-400" />
            <div>
              <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-200">
                MCP Setup
              </h1>
              <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">
                One-click install for Claude Code, Cursor, and Windsurf
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={refresh}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded text-sm font-medium
              border border-slate-300 dark:border-slate-700 text-slate-600 dark:text-slate-400
              hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-40 transition-colors"
            aria-label="Refresh host detection"
          >
            <RefreshCw className={cls('w-3.5 h-3.5', isLoading && 'animate-spin')} />
            Refresh
          </button>
        </div>

        {/* MCP port info */}
        {data?.mcp_port !== undefined && data.mcp_port > 0 && (
          <p className="text-xs text-slate-400 dark:text-slate-500">
            Daemon MCP port:{' '}
            <code className="font-mono text-slate-600 dark:text-slate-300">
              {data.mcp_port}
            </code>
          </p>
        )}

        {/* Loading */}
        {isLoading && (
          <div className="flex items-center justify-center py-12 text-slate-400">
            <RefreshCw className="w-5 h-5 animate-spin mr-2" />
            Detecting hosts…
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="rounded border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-950/30 px-4 py-3 text-sm text-red-600 dark:text-red-400">
            Failed to detect hosts: {String(error)}
          </div>
        )}

        {/* No hosts welcome banner */}
        {!isLoading && !error && allAbsent && <NoHostsBanner />}

        {/* Host cards */}
        {!isLoading &&
          !error &&
          data?.hosts
            .filter((h) => h.state !== 'host_absent')
            .map((host) => (
              <HostCard key={host.id} host={host} onRefresh={refresh} />
            ))}

        {/* Show absent hosts collapsed */}
        {!isLoading && !error && data && (
          <AbsentHostsCollapsed
            hosts={data.hosts.filter((h) => h.state === 'host_absent')}
          />
        )}

        {/* How it works */}
        <div className="border border-slate-200 dark:border-slate-800 rounded-lg px-4 py-4 text-xs text-slate-400 dark:text-slate-500 space-y-1.5">
          <p className="font-medium text-slate-500 dark:text-slate-400">How it works</p>
          <ul className="list-disc list-inside space-y-1">
            <li>
              <strong>Install</strong> — merges <code className="font-mono">archigraph mcp</code>{' '}
              into your host's MCP config file (idempotent).
            </li>
            <li>
              <strong>Uninstall</strong> — removes only the archigraph entry; other servers are
              preserved.
            </li>
            <li>
              <strong>Verify</strong> — pings the local MCP server to confirm it's reachable.
            </li>
            <li>A <code className="font-mono">.bak</code> backup is created before any write.</li>
          </ul>
        </div>
      </div>
    </div>
  )
}

// ── Collapsed absent hosts section ────────────────────────────────────────────

function AbsentHostsCollapsed({ hosts }: { hosts: MCPHostInfo[] }) {
  const [open, setOpen] = useState(false)
  if (hosts.length === 0) return null
  return (
    <div className="border border-slate-100 dark:border-slate-900 rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-2 px-4 py-2.5 text-left text-xs
          text-slate-400 dark:text-slate-600 hover:text-slate-500 dark:hover:text-slate-500
          bg-slate-50 dark:bg-slate-900/50 transition-colors"
      >
        {open ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
        {hosts.length} host{hosts.length > 1 ? 's' : ''} not detected on this machine
      </button>
      {open && (
        <div className="px-4 pb-3 pt-1 space-y-1 bg-white dark:bg-slate-950">
          {hosts.map((h) => (
            <div key={h.id} className="flex items-center gap-2 text-xs text-slate-400">
              <XCircle className="w-3 h-3 flex-shrink-0" />
              <span>{h.label}</span>
              {h.config_path && (
                <span className="font-mono text-slate-300 dark:text-slate-700 truncate">
                  {h.config_path}
                </span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
