/**
 * BackendGroup — Top-level collapsible section for Paths v2 backend grouping (#1219).
 *
 * Shows:
 *  - Caret for expand/collapse
 *  - Backend name (repo slug)
 *  - Service type chip (REST / gRPC / GraphQL)
 *  - Count badge (number of endpoint definitions in this backend)
 *  - Health chip: ANY-rate indicator (post-#1126 method resolution)
 *  - Cross-backend reference indicator (called from another backend's repo)
 *
 * Children render the existing controller-level PathsGroup components.
 *
 * Collapse state is persisted to localStorage per backend name.
 */

import { ChevronRight, ArrowLeftRight, AlertTriangle } from 'lucide-react'
import type { BackendInfo } from '@/types/api'

// ── localStorage helpers ──────────────────────────────────────────────────────

const LS_PREFIX = 'paths-backend-collapsed:'

function readCollapsed(name: string, defaultCollapsed: boolean): boolean {
  try {
    const raw = localStorage.getItem(`${LS_PREFIX}${name}`)
    if (raw === null) return defaultCollapsed
    return raw === 'true'
  } catch {
    return defaultCollapsed
  }
}

export function writeCollapsed(name: string, collapsed: boolean): void {
  try {
    localStorage.setItem(`${LS_PREFIX}${name}`, String(collapsed))
  } catch { /* noop */ }
}

export { readCollapsed }

// ── ServiceType chip ──────────────────────────────────────────────────────────

const SERVICE_TYPE_COLORS: Record<string, string> = {
  REST:    'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border-sky-200 dark:border-sky-800',
  gRPC:    'bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 border-purple-200 dark:border-purple-800',
  GraphQL: 'bg-pink-100 dark:bg-pink-900/40 text-pink-700 dark:text-pink-300 border-pink-200 dark:border-pink-800',
}

function ServiceTypeChip({ type }: { type: string }) {
  const cls = SERVICE_TYPE_COLORS[type] ?? 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700'
  return (
    <span
      className={`inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium rounded border ${cls} flex-shrink-0`}
      aria-label={`Service type: ${type}`}
    >
      {type}
    </span>
  )
}

// ── Health chip: ANY-rate ─────────────────────────────────────────────────────

/**
 * Shows a warning chip when any_rate > 10% — indicates many endpoints whose HTTP
 * method resolved to ANY (often means unresolved dynamic routing).
 */
function HealthChip({ anyRate }: { anyRate: number }) {
  if (anyRate < 0.1) return null
  const pct = Math.round(anyRate * 100)
  return (
    <span
      className="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] font-medium rounded border bg-amber-50 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 border-amber-200 dark:border-amber-800 flex-shrink-0"
      title={`${pct}% of endpoints resolved to ANY method — may indicate dynamic routing`}
      aria-label={`Health warning: ${pct}% ANY-rate`}
    >
      <AlertTriangle className="w-2.5 h-2.5" aria-hidden />
      {pct}% ANY
    </span>
  )
}

// ── Cross-backend reference indicator ────────────────────────────────────────

function CrossBackendBadge() {
  return (
    <span
      className="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] font-medium rounded border bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 border-indigo-200 dark:border-indigo-800 flex-shrink-0"
      title="Endpoints in this backend are called from another backend's repository"
      aria-label="Cross-backend references detected"
    >
      <ArrowLeftRight className="w-2.5 h-2.5" aria-hidden />
      cross-repo
    </span>
  )
}

// ── BackendGroup ──────────────────────────────────────────────────────────────

interface BackendGroupProps {
  backend: BackendInfo
  isExpanded: boolean
  onToggle: () => void
  children: React.ReactNode
}

export function BackendGroup({ backend, isExpanded, onToggle, children }: BackendGroupProps) {
  const { name, service_type, count, any_rate, has_cross_backend_refs } = backend

  return (
    <div data-backend={name}>
      {/* Backend section header */}
      <button
        type="button"
        className={[
          'w-full flex items-center gap-2 px-3 py-2.5',
          'bg-slate-200/70 dark:bg-slate-800/70 border-b border-slate-300 dark:border-slate-700',
          'hover:bg-slate-200 dark:hover:bg-slate-800 focus:outline-none focus:bg-slate-200 dark:focus:bg-slate-800',
          'transition-colors duration-75 cursor-pointer',
          'sticky top-0 z-20',
        ].join(' ')}
        onClick={onToggle}
        aria-expanded={isExpanded}
        aria-label={`Backend ${name} — ${count} endpoints`}
      >
        {/* Caret */}
        <ChevronRight
          className={[
            'w-4 h-4 text-slate-500 dark:text-slate-400 flex-shrink-0',
            'transition-transform duration-150',
            isExpanded ? 'rotate-90' : '',
          ].join(' ')}
          aria-hidden
        />

        {/* Backend name */}
        <span className="flex-1 min-w-0 text-left text-sm font-semibold text-slate-900 dark:text-slate-100 truncate font-mono">
          {name}
        </span>

        {/* Chips */}
        {service_type && <ServiceTypeChip type={service_type} />}
        {has_cross_backend_refs && <CrossBackendBadge />}
        {any_rate !== undefined && any_rate > 0 && <HealthChip anyRate={any_rate} />}

        {/* Count badge */}
        <span
          className="flex-shrink-0 text-xs tabular-nums text-slate-500 dark:text-slate-300 bg-slate-300 dark:bg-slate-700 border border-slate-400 dark:border-slate-600 rounded px-1.5 py-0.5 ml-1 font-medium"
          title={`${count} endpoint definitions`}
        >
          {count}
        </span>
      </button>

      {/* Controller groups inside this backend */}
      {isExpanded && (
        <div role="rowgroup" data-backend-content={name}>
          {children}
        </div>
      )}
    </div>
  )
}
