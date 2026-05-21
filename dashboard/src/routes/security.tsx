/**
 * Security surface (#1330)
 *
 * URL: /security/:group
 *
 * Four tabs:
 *   Auth Coverage  — HTTP endpoints without auth middleware
 *   Secrets        — Hardcoded credentials + secrets-manager integrations
 *   N+1 Queries    — ORM queries inside loops
 *   Import Cycles  — Circular import dependencies
 *
 * Features:
 *   - Severity-grouped findings (error / warn / info)
 *   - Per-finding row with file:line + remediation hint
 *   - Filter by severity, file, kind
 *   - Stats header with per-category counts
 *   - Export all findings as CSV / Markdown
 */

import { useState, useCallback, useMemo } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  ShieldAlert, ShieldCheck, ShieldX,
  KeyRound, AlertTriangle, Info,
  RefreshCw, Download, XCircle,
  CheckCircle2, ArrowRightLeft, Database,
  ChevronDown, ChevronRight, Filter,
} from 'lucide-react'
import {
  fetchSecurityAuthCoverage,
  fetchSecuritySecrets,
  fetchSecurityCycles,
  fetchSecurityNPlusOne,
  type AuthEndpointFinding,
  type SecretFinding,
  type CycleFinding,
  type NPlusOneFindingFE,
} from '@/api/client'

// ─────────────────────────────────────────────────────────────────────────────
// Constants
// ─────────────────────────────────────────────────────────────────────────────

const GROUP_DEFAULT = 'fixture-a'
const STALE_MS = 60_000

type TabId = 'auth' | 'secrets' | 'nplus1' | 'cycles'

// ─────────────────────────────────────────────────────────────────────────────
// Severity helpers
// ─────────────────────────────────────────────────────────────────────────────

function SeverityBadge({ severity }: { severity: string }) {
  const map: Record<string, string> = {
    error: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-700',
    warn:  'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-700',
    info:  'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border border-sky-200 dark:border-sky-700',
  }
  const icons: Record<string, React.ReactNode> = {
    error: <XCircle className="w-3 h-3" />,
    warn:  <AlertTriangle className="w-3 h-3" />,
    info:  <Info className="w-3 h-3" />,
  }
  const cls = map[severity] ?? map['info']
  return (
    <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-semibold ${cls}`}>
      {icons[severity]}
      {severity}
    </span>
  )
}

function SeverityIcon({ severity }: { severity: string }) {
  if (severity === 'error') return <XCircle className="w-4 h-4 text-red-500 shrink-0" />
  if (severity === 'warn') return <AlertTriangle className="w-4 h-4 text-amber-500 shrink-0" />
  return <Info className="w-4 h-4 text-sky-500 shrink-0" />
}

// ─────────────────────────────────────────────────────────────────────────────
// Stats header card
// ─────────────────────────────────────────────────────────────────────────────

interface StatCardProps {
  label: string
  value: number | string
  sub?: string
  color?: string
}

function StatCard({ label, value, sub, color = 'text-slate-900 dark:text-slate-100' }: StatCardProps) {
  return (
    <div className="bg-white dark:bg-slate-800 rounded-lg border border-slate-200 dark:border-slate-700 px-4 py-3">
      <p className="text-xs text-slate-500 dark:text-slate-400 font-medium uppercase tracking-wide mb-0.5">{label}</p>
      <p className={`text-2xl font-bold tabular-nums ${color}`}>{value}</p>
      {sub && <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{sub}</p>}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Filter bar
// ─────────────────────────────────────────────────────────────────────────────

interface FilterBarProps {
  severity: string
  onSeverity: (v: string) => void
  file: string
  onFile: (v: string) => void
  placeholder?: string
}

function FilterBar({ severity, onSeverity, file, onFile, placeholder }: FilterBarProps) {
  return (
    <div className="flex items-center gap-2 flex-wrap">
      <Filter className="w-3.5 h-3.5 text-slate-400 shrink-0" />
      <select
        value={severity}
        onChange={(e) => onSeverity(e.target.value)}
        className="text-xs border border-slate-200 dark:border-slate-700 rounded px-2 py-1
                   bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-300
                   focus:outline-none focus:ring-1 focus:ring-sky-400"
      >
        <option value="">All severities</option>
        <option value="error">Error</option>
        <option value="warn">Warn</option>
        <option value="info">Info</option>
      </select>
      <input
        type="text"
        value={file}
        onChange={(e) => onFile(e.target.value)}
        placeholder={placeholder ?? 'Filter by file…'}
        className="text-xs border border-slate-200 dark:border-slate-700 rounded px-2 py-1
                   bg-white dark:bg-slate-800 text-slate-700 dark:text-slate-300
                   placeholder-slate-400 dark:placeholder-slate-600
                   focus:outline-none focus:ring-1 focus:ring-sky-400 w-48"
      />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Finding row — generic
// ─────────────────────────────────────────────────────────────────────────────

interface FindingRowProps {
  severity: string
  primary: string
  secondary?: string
  file?: string
  line?: number
  badge?: React.ReactNode
  detail?: React.ReactNode
}

function FindingRow({ severity, primary, secondary, file, line, badge, detail }: FindingRowProps) {
  const [open, setOpen] = useState(false)
  return (
    <div className="border-b border-slate-100 dark:border-slate-800 last:border-0">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-start gap-3 px-4 py-2.5 text-left hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
      >
        <span className="mt-0.5"><SeverityIcon severity={severity} /></span>
        <span className="flex-1 min-w-0">
          <span className="text-sm font-medium text-slate-800 dark:text-slate-200 truncate block">{primary}</span>
          {secondary && <span className="text-xs text-slate-500 dark:text-slate-400 truncate block">{secondary}</span>}
          {file && (
            <span className="text-xs font-mono text-slate-400 dark:text-slate-500 truncate block">
              {file}{line ? `:${line}` : ''}
            </span>
          )}
        </span>
        <span className="flex items-center gap-2 shrink-0">
          {badge}
          {open
            ? <ChevronDown className="w-3.5 h-3.5 text-slate-400" />
            : <ChevronRight className="w-3.5 h-3.5 text-slate-400" />
          }
        </span>
      </button>
      {open && detail && (
        <div className="px-11 pb-3">
          {detail}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab: Auth Coverage
// ─────────────────────────────────────────────────────────────────────────────

function AuthTab({ group }: { group: string }) {
  const [severityFilter, setSeverityFilter] = useState('')
  const [fileFilter, setFileFilter] = useState('')
  const [onlyUncovered, setOnlyUncovered] = useState(false)

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['security', 'auth-coverage', group],
    queryFn: () => fetchSecurityAuthCoverage(group),
    staleTime: STALE_MS,
  })

  const filtered = useMemo(() => {
    if (!data) return []
    return data.findings.filter((f) => {
      if (severityFilter && f.severity !== severityFilter) return false
      if (fileFilter && !(f.source_file ?? '').includes(fileFilter)) return false
      if (onlyUncovered && f.has_auth) return false
      return true
    })
  }, [data, severityFilter, fileFilter, onlyUncovered])

  if (isLoading) return <LoadingPanel />
  if (isError || !data) return <ErrorPanel onRetry={refetch} />

  return (
    <div className="space-y-4">
      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="Total Endpoints" value={data.total_endpoints} />
        <StatCard
          label="Coverage"
          value={`${data.coverage_pct.toFixed(1)}%`}
          color={data.coverage_pct >= 80 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}
        />
        <StatCard label="Errors" value={data.error_count} color="text-red-600 dark:text-red-400" />
        <StatCard label="Warnings" value={data.warn_count} color="text-amber-600 dark:text-amber-400" />
      </div>

      {/* Filters */}
      <div className="flex items-center gap-4 flex-wrap">
        <FilterBar
          severity={severityFilter}
          onSeverity={setSeverityFilter}
          file={fileFilter}
          onFile={setFileFilter}
          placeholder="Filter by file…"
        />
        <label className="flex items-center gap-1.5 text-xs text-slate-600 dark:text-slate-400 cursor-pointer">
          <input
            type="checkbox"
            checked={onlyUncovered}
            onChange={(e) => setOnlyUncovered(e.target.checked)}
            className="rounded border-slate-300 dark:border-slate-600 text-sky-500 focus:ring-sky-400"
          />
          Uncovered only
        </label>
        <ExportButton
          label="Export"
          onCSV={() => exportAuthCSV(data.findings)}
          onMarkdown={() => exportAuthMarkdown(data.findings)}
        />
      </div>

      {/* Findings */}
      <FindingsList count={filtered.length}>
        {filtered.map((f) => (
          <FindingRow
            key={f.entity_id}
            severity={f.severity}
            primary={[f.method, f.path].filter(Boolean).join(' ') || f.name}
            secondary={f.name}
            file={f.source_file}
            line={f.start_line}
            badge={
              <span className="flex gap-1">
                {f.sensitive_op && (
                  <span className="text-[10px] px-1 rounded bg-red-100 dark:bg-red-900/40 text-red-600 dark:text-red-300 border border-red-200 dark:border-red-700 font-semibold">
                    sensitive
                  </span>
                )}
                {f.idor_risk && (
                  <span className="text-[10px] px-1 rounded bg-orange-100 dark:bg-orange-900/40 text-orange-600 dark:text-orange-300 border border-orange-200 dark:border-orange-700 font-semibold">
                    IDOR
                  </span>
                )}
                <SeverityBadge severity={f.severity} />
              </span>
            }
            detail={
              <div className="text-xs space-y-1 text-slate-600 dark:text-slate-400">
                <div><span className="font-semibold">Entity ID:</span> {f.entity_id}</div>
                <div><span className="font-semibold">Repo:</span> {f.repo}</div>
                {f.has_auth && f.auth_evidence && (
                  <div><span className="font-semibold">Auth evidence:</span> {f.auth_evidence}</div>
                )}
                {!f.has_auth && (
                  <div className="text-amber-600 dark:text-amber-400 font-medium">
                    No auth middleware or decorator detected. Add authentication guard to this endpoint.
                  </div>
                )}
              </div>
            }
          />
        ))}
      </FindingsList>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab: Secrets
// ─────────────────────────────────────────────────────────────────────────────

function SecretsTab({ group }: { group: string }) {
  const [severityFilter, setSeverityFilter] = useState('')
  const [fileFilter, setFileFilter] = useState('')

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['security', 'secrets', group],
    queryFn: () => fetchSecuritySecrets(group),
    staleTime: STALE_MS,
  })

  const filtered = useMemo(() => {
    if (!data) return []
    return data.findings.filter((f) => {
      if (severityFilter && f.severity !== severityFilter) return false
      if (fileFilter && !(f.source_file ?? '').includes(fileFilter)) return false
      return true
    })
  }, [data, severityFilter, fileFilter])

  if (isLoading) return <LoadingPanel />
  if (isError || !data) return <ErrorPanel onRetry={refetch} />

  return (
    <div className="space-y-4">
      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="Total Findings" value={data.total_findings} />
        <StatCard label="Errors" value={data.error_count} color="text-red-600 dark:text-red-400" />
        <StatCard label="Warnings" value={data.warn_count} color="text-amber-600 dark:text-amber-400" />
        <StatCard label="Info (SM integrations)" value={data.info_count} color="text-sky-600 dark:text-sky-400" />
      </div>

      {/* By category */}
      {Object.keys(data.by_category).length > 0 && (
        <div className="flex gap-2 flex-wrap">
          {Object.entries(data.by_category).map(([cat, n]) => (
            <span
              key={cat}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs
                         bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400
                         border border-slate-200 dark:border-slate-700"
            >
              {cat}: <span className="font-bold">{n}</span>
            </span>
          ))}
        </div>
      )}

      {/* Filters */}
      <div className="flex items-center gap-4 flex-wrap">
        <FilterBar
          severity={severityFilter}
          onSeverity={setSeverityFilter}
          file={fileFilter}
          onFile={setFileFilter}
        />
        <ExportButton
          label="Export"
          onCSV={() => exportSecretsCSV(data.findings)}
          onMarkdown={() => exportSecretsMarkdown(data.findings)}
        />
      </div>

      {/* Findings */}
      <FindingsList count={filtered.length}>
        {filtered.map((f) => (
          <FindingRow
            key={f.entity_id}
            severity={f.severity}
            primary={f.category === 'hardcoded_credential' ? 'Hardcoded credential detected' : `Secrets manager: ${f.provider || f.name}`}
            secondary={f.category === 'secrets_management' ? 'Proper secrets manager integration found' : `Name: ${f.name}`}
            file={f.source_file}
            line={f.start_line}
            badge={<SeverityBadge severity={f.severity} />}
            detail={
              <div className="text-xs space-y-1 text-slate-600 dark:text-slate-400">
                <div><span className="font-semibold">Entity ID:</span> {f.entity_id}</div>
                <div><span className="font-semibold">Repo:</span> {f.repo}</div>
                {f.language && <div><span className="font-semibold">Language:</span> {f.language}</div>}
                {f.category === 'secrets_management' && f.provider && (
                  <div><span className="font-semibold">Provider:</span> {f.provider}</div>
                )}
                {f.remediation && (
                  <div className="text-amber-600 dark:text-amber-400 font-medium">
                    Remediation: {f.remediation.replace(/_/g, ' ')}
                  </div>
                )}
              </div>
            }
          />
        ))}
      </FindingsList>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab: N+1 Queries
// ─────────────────────────────────────────────────────────────────────────────

function NPlusOneTab({ group }: { group: string }) {
  const [severityFilter, setSeverityFilter] = useState('')
  const [fileFilter, setFileFilter] = useState('')

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['security', 'nplus1', group],
    queryFn: () => fetchSecurityNPlusOne(group),
    staleTime: STALE_MS,
  })

  // Infer severity per finding (error if no suggestion, else warn)
  const findings: NPlusOneFindingFE[] = useMemo(() => {
    if (!data) return []
    return data.findings.map((f) => ({
      ...f,
      severity: (f.severity ?? 'warn') as 'error' | 'warn' | 'info',
    }))
  }, [data])

  const filtered = useMemo(() => {
    return findings.filter((f) => {
      if (severityFilter && f.severity !== severityFilter) return false
      if (fileFilter && !f.query_file.includes(fileFilter) && !f.caller_file.includes(fileFilter)) return false
      return true
    })
  }, [findings, severityFilter, fileFilter])

  if (isLoading) return <LoadingPanel />
  if (isError || !data) return <ErrorPanel onRetry={refetch} />

  const errorCount = findings.filter((f) => f.severity === 'error').length
  const warnCount = findings.filter((f) => f.severity === 'warn').length

  return (
    <div className="space-y-4">
      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="Total Findings" value={data.total_findings} />
        <StatCard label="Errors" value={errorCount} color="text-red-600 dark:text-red-400" />
        <StatCard label="Warnings" value={warnCount} color="text-amber-600 dark:text-amber-400" />
        <StatCard
          label="By ORM"
          value={Object.keys(data.by_orm).length}
          sub={Object.keys(data.by_orm).join(', ') || '—'}
        />
      </div>

      {/* Filters */}
      <div className="flex items-center gap-4 flex-wrap">
        <FilterBar
          severity={severityFilter}
          onSeverity={setSeverityFilter}
          file={fileFilter}
          onFile={setFileFilter}
          placeholder="Filter by file…"
        />
        <ExportButton
          label="Export"
          onCSV={() => exportNPlusOneCSV(data.findings)}
          onMarkdown={() => exportNPlusOneMarkdown(data.findings)}
        />
      </div>

      {/* Findings */}
      <FindingsList count={filtered.length}>
        {filtered.map((f, idx) => (
          <FindingRow
            key={`${f.caller_entity_id}-${idx}`}
            severity={f.severity}
            primary={`${f.caller_name}() — query in loop`}
            secondary={f.suggestion}
            file={f.query_file}
            line={f.query_line}
            badge={
              <span className="flex gap-1">
                {f.orm && (
                  <span className="text-[10px] px-1 rounded bg-violet-100 dark:bg-violet-900/40 text-violet-600 dark:text-violet-300 border border-violet-200 dark:border-violet-700 font-semibold uppercase">
                    {f.orm}
                  </span>
                )}
                <SeverityBadge severity={f.severity} />
              </span>
            }
            detail={
              <div className="text-xs space-y-1 text-slate-600 dark:text-slate-400">
                <div><span className="font-semibold">Caller:</span> {f.caller_name} at {f.caller_file}:{f.caller_start_line}</div>
                <div><span className="font-semibold">Query:</span> {f.query_name} at {f.query_file}:{f.query_line}</div>
                {f.loop_subtype && <div><span className="font-semibold">Loop kind:</span> {f.loop_subtype}</div>}
                {f.language && <div><span className="font-semibold">Language:</span> {f.language}</div>}
                {f.suggestion && (
                  <div className="text-amber-600 dark:text-amber-400 font-medium mt-1">
                    Suggestion: {f.suggestion}
                  </div>
                )}
              </div>
            }
          />
        ))}
      </FindingsList>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab: Import Cycles
// ─────────────────────────────────────────────────────────────────────────────

function CyclesTab({ group }: { group: string }) {
  const [severityFilter, setSeverityFilter] = useState('')

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['security', 'cycles', group],
    queryFn: () => fetchSecurityCycles(group),
    staleTime: STALE_MS,
  })

  const filtered = useMemo(() => {
    if (!data) return []
    return data.findings.filter((f) => {
      if (severityFilter && f.severity !== severityFilter) return false
      return true
    })
  }, [data, severityFilter])

  if (isLoading) return <LoadingPanel />
  if (isError || !data) return <ErrorPanel onRetry={refetch} />

  return (
    <div className="space-y-4">
      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
        <StatCard label="Total Cycles" value={data.total_cycles} />
        <StatCard label="Errors (>5 members)" value={data.error_count} color="text-red-600 dark:text-red-400" />
        <StatCard label="Warnings (3–5)" value={data.warn_count} color="text-amber-600 dark:text-amber-400" />
      </div>

      {/* Filters */}
      <div className="flex items-center gap-4 flex-wrap">
        <FilterBar
          severity={severityFilter}
          onSeverity={setSeverityFilter}
          file=""
          onFile={() => {}}
          placeholder=""
        />
        <ExportButton
          label="Export"
          onCSV={() => exportCyclesCSV(data.findings)}
          onMarkdown={() => exportCyclesMarkdown(data.findings)}
        />
      </div>

      {/* Findings */}
      <FindingsList count={filtered.length}>
        {filtered.map((f, idx) => (
          <FindingRow
            key={`${f.repo}-${idx}`}
            severity={f.severity}
            primary={`${f.size}-member cycle in ${f.repo}`}
            secondary={f.members.slice(0, 3).join(' → ') + (f.members.length > 3 ? ' → …' : '')}
            badge={
              <span className="flex gap-1">
                <span className="text-[10px] px-1 rounded bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700 font-semibold">
                  {f.size} nodes
                </span>
                <SeverityBadge severity={f.severity} />
              </span>
            }
            detail={
              <div className="text-xs space-y-1.5 text-slate-600 dark:text-slate-400">
                <div><span className="font-semibold">Members:</span></div>
                <ul className="ml-2 space-y-0.5 font-mono">
                  {f.members.map((m) => (
                    <li key={m} className="text-slate-500 dark:text-slate-500">{m}</li>
                  ))}
                </ul>
                {f.weakest_link_from_id && (
                  <div className="text-amber-600 dark:text-amber-400 font-medium mt-1">
                    Suggested break: {f.weakest_link_from_id} → {f.weakest_link_to_id}
                  </div>
                )}
                {f.suggested_extraction_id && (
                  <div className="text-sky-600 dark:text-sky-400">
                    Extraction candidate: {f.suggested_extraction_id}
                  </div>
                )}
              </div>
            }
          />
        ))}
      </FindingsList>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared: FindingsList wrapper
// ─────────────────────────────────────────────────────────────────────────────

function FindingsList({ count, children }: { count: number; children: React.ReactNode }) {
  if (count === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-slate-500 dark:text-slate-400">
        <CheckCircle2 className="w-10 h-10 text-emerald-400 mb-3" />
        <p className="text-sm font-medium">No findings</p>
        <p className="text-xs mt-1">All clear — no issues match the current filters.</p>
      </div>
    )
  }
  return (
    <div className="bg-white dark:bg-slate-900 rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden">
      <div className="px-4 py-2 border-b border-slate-100 dark:border-slate-800 text-xs text-slate-500 dark:text-slate-400">
        {count} finding{count === 1 ? '' : 's'}
      </div>
      {children}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared: Loading / Error panels
// ─────────────────────────────────────────────────────────────────────────────

function LoadingPanel() {
  return (
    <div className="flex items-center justify-center py-16 text-slate-400 dark:text-slate-600">
      <RefreshCw className="w-5 h-5 animate-spin mr-2" />
      <span className="text-sm">Loading…</span>
    </div>
  )
}

function ErrorPanel({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-slate-500 dark:text-slate-400 gap-3">
      <ShieldX className="w-10 h-10 text-red-400" />
      <p className="text-sm">Failed to load findings</p>
      <button
        type="button"
        onClick={onRetry}
        className="text-xs px-3 py-1.5 rounded bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
      >
        Retry
      </button>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Export helpers
// ─────────────────────────────────────────────────────────────────────────────

function downloadText(content: string, filename: string) {
  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

function exportAuthCSV(findings: AuthEndpointFinding[]) {
  const hdr = 'severity,method,path,source_file,start_line,has_auth,sensitive_op,idor_risk,repo'
  const rows = findings.map((f) =>
    [f.severity, f.method, f.path, f.source_file, f.start_line, f.has_auth, f.sensitive_op, f.idor_risk, f.repo].join(','),
  )
  downloadText([hdr, ...rows].join('\n'), 'auth-coverage.csv')
}

function exportAuthMarkdown(findings: AuthEndpointFinding[]) {
  const lines = ['# Auth Coverage Findings', '', '| Severity | Method | Path | File | Auth? |', '|---|---|---|---|---|']
  findings.forEach((f) => {
    lines.push(`| ${f.severity} | ${f.method ?? ''} | ${f.path ?? ''} | ${f.source_file ?? ''}:${f.start_line ?? ''} | ${f.has_auth ? '✓' : '✗'} |`)
  })
  downloadText(lines.join('\n'), 'auth-coverage.md')
}

function exportSecretsCSV(findings: SecretFinding[]) {
  const hdr = 'severity,category,name,source_file,start_line,language,provider,repo'
  const rows = findings.map((f) =>
    [f.severity, f.category, f.name, f.source_file, f.start_line, f.language, f.provider, f.repo].join(','),
  )
  downloadText([hdr, ...rows].join('\n'), 'secrets.csv')
}

function exportSecretsMarkdown(findings: SecretFinding[]) {
  const lines = ['# Secrets Findings', '', '| Severity | Category | File | Language |', '|---|---|---|---|']
  findings.forEach((f) => {
    lines.push(`| ${f.severity} | ${f.category} | ${f.source_file ?? ''}:${f.start_line ?? ''} | ${f.language ?? ''} |`)
  })
  downloadText(lines.join('\n'), 'secrets.md')
}

function exportNPlusOneCSV(findings: NPlusOneFindingFE[]) {
  const hdr = 'caller_name,caller_file,caller_line,query_name,query_file,query_line,orm,language'
  const rows = findings.map((f) =>
    [f.caller_name, f.caller_file, f.caller_start_line, f.query_name, f.query_file, f.query_line, f.orm, f.language].join(','),
  )
  downloadText([hdr, ...rows].join('\n'), 'nplus1.csv')
}

function exportNPlusOneMarkdown(findings: NPlusOneFindingFE[]) {
  const lines = ['# N+1 Query Findings', '', '| Caller | Query | ORM | Suggestion |', '|---|---|---|---|']
  findings.forEach((f) => {
    lines.push(`| ${f.caller_name} (${f.caller_file}:${f.caller_start_line}) | ${f.query_name} (${f.query_file}:${f.query_line}) | ${f.orm} | ${f.suggestion} |`)
  })
  downloadText(lines.join('\n'), 'nplus1.md')
}

function exportCyclesCSV(findings: CycleFinding[]) {
  const hdr = 'severity,size,repo,members,weakest_from,weakest_to'
  const rows = findings.map((f) =>
    [f.severity, f.size, f.repo, f.members.join('|'), f.weakest_link_from_id ?? '', f.weakest_link_to_id ?? ''].join(','),
  )
  downloadText([hdr, ...rows].join('\n'), 'import-cycles.csv')
}

function exportCyclesMarkdown(findings: CycleFinding[]) {
  const lines = ['# Import Cycle Findings', '']
  findings.forEach((f, i) => {
    lines.push(`## Cycle ${i + 1} (${f.severity}, ${f.size} nodes, ${f.repo})`)
    lines.push('')
    lines.push('Members:')
    f.members.forEach((m) => lines.push(`- ${m}`))
    if (f.weakest_link_from_id) {
      lines.push('')
      lines.push(`Suggested break: \`${f.weakest_link_from_id}\` → \`${f.weakest_link_to_id}\``)
    }
    lines.push('')
  })
  downloadText(lines.join('\n'), 'import-cycles.md')
}

// ─────────────────────────────────────────────────────────────────────────────
// Export button with dropdown
// ─────────────────────────────────────────────────────────────────────────────

function ExportButton({ label, onCSV, onMarkdown }: { label: string; onCSV: () => void; onMarkdown: () => void }) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 text-xs px-2.5 py-1.5 rounded bg-slate-100 dark:bg-slate-800
                   text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700
                   border border-slate-200 dark:border-slate-700 transition-colors"
      >
        <Download className="w-3 h-3" />
        {label}
        <ChevronDown className="w-3 h-3 opacity-60" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={close} />
          <div className="absolute right-0 top-full mt-1 z-20 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg py-1 min-w-[140px]">
            <button
              type="button"
              onClick={() => { onCSV(); close() }}
              className="w-full text-left px-3 py-1.5 text-xs text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
            >
              Export as CSV
            </button>
            <button
              type="button"
              onClick={() => { onMarkdown(); close() }}
              className="w-full text-left px-3 py-1.5 text-xs text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800 transition-colors"
            >
              Export as Markdown
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab bar
// ─────────────────────────────────────────────────────────────────────────────

interface TabConfig {
  id: TabId
  label: string
  icon: React.ReactNode
  queryKey: (group: string) => readonly unknown[]
}

const TABS: TabConfig[] = [
  { id: 'auth',    label: 'Auth Coverage', icon: <ShieldAlert className="w-4 h-4" />, queryKey: (g) => ['security', 'auth-coverage', g] },
  { id: 'secrets', label: 'Secrets',       icon: <KeyRound    className="w-4 h-4" />, queryKey: (g) => ['security', 'secrets', g] },
  { id: 'nplus1',  label: 'N+1 Queries',   icon: <Database    className="w-4 h-4" />, queryKey: (g) => ['security', 'nplus1', g] },
  { id: 'cycles',  label: 'Import Cycles', icon: <ArrowRightLeft className="w-4 h-4" />, queryKey: (g) => ['security', 'cycles', g] },
]

// ─────────────────────────────────────────────────────────────────────────────
// Root route component
// ─────────────────────────────────────────────────────────────────────────────

export function SecurityRoute() {
  const { group = GROUP_DEFAULT } = useParams()
  const [activeTab, setActiveTab] = useState<TabId>('auth')

  const tabCls = (id: TabId) =>
    [
      'flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors',
      id === activeTab
        ? 'border-sky-500 text-sky-600 dark:text-sky-400'
        : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300 hover:border-slate-300 dark:hover:border-slate-600',
    ].join(' ')

  return (
    <div className="flex flex-col h-full">
      {/* Page header */}
      <div className="flex items-center gap-3 px-6 pt-5 pb-3 border-b border-slate-200 dark:border-slate-800 shrink-0">
        <ShieldCheck className="w-5 h-5 text-sky-500" />
        <div>
          <h1 className="text-base font-semibold text-slate-900 dark:text-slate-100">Security &amp; Quality</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
            Group: <span className="font-mono font-medium">{group}</span>
          </p>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex border-b border-slate-200 dark:border-slate-800 px-4 shrink-0 overflow-x-auto">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            data-testid={`security-tab-${tab.id}`}
            className={tabCls(tab.id)}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.icon}
            <span className="hidden sm:inline">{tab.label}</span>
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto px-6 py-5">
        {activeTab === 'auth'    && <AuthTab    group={group} />}
        {activeTab === 'secrets' && <SecretsTab group={group} />}
        {activeTab === 'nplus1'  && <NPlusOneTab group={group} />}
        {activeTab === 'cycles'  && <CyclesTab  group={group} />}
      </div>
    </div>
  )
}
