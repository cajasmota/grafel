/**
 * FlowsGroup — Collapsible section header for entry-kind groups (#1151).
 *
 * Shows:
 *  - Caret (left) for expand/collapse
 *  - Kind icon + label
 *  - Count badge (right)
 *
 * Children (FlowRow list) are rendered when expanded.
 * Collapsed state is persisted to localStorage keyed
 * `archigraph:flows-group-{kind}`.
 */

import { ChevronRight, Globe, Radio, Clock, Layout, TestTube2, Terminal, FunctionSquare, Cpu } from 'lucide-react'
import type { FlowEntryKind, FlowPriorityHint } from '@/types/api'

// ── Icon map ─────────────────────────────────────────────────────────────────

const KIND_ICONS: Record<FlowEntryKind, React.FC<{ className?: string }>> = {
  http_handler:     Globe,
  message_consumer: Radio,
  scheduled_task:   Clock,
  component_render: Layout,
  test:             TestTube2,
  cli_command:      Terminal,
  function:         FunctionSquare,
  internal:         Cpu,
}

// ── Priority colour accent for the group header ───────────────────────────────

const PRIORITY_ACCENT: Record<FlowPriorityHint, string> = {
  high:   'text-sky-500 dark:text-sky-400',
  medium: 'text-violet-500 dark:text-violet-400',
  low:    'text-slate-400 dark:text-slate-500',
}

// ── Component ─────────────────────────────────────────────────────────────────

interface FlowsGroupProps {
  kind: FlowEntryKind
  label: string
  priority: FlowPriorityHint
  count: number
  isExpanded: boolean
  onToggle: () => void
  children: React.ReactNode
}

export function FlowsGroup({
  kind,
  label,
  priority,
  count,
  isExpanded,
  onToggle,
  children,
}: FlowsGroupProps) {
  const Icon = KIND_ICONS[kind] ?? Cpu
  const accentClass = PRIORITY_ACCENT[priority]

  return (
    <div data-flow-group={kind}>
      {/* Group header */}
      <button
        type="button"
        className={[
          'w-full flex items-center gap-2 px-3 py-2',
          'bg-slate-100/80 dark:bg-slate-900/80 border-b border-slate-200 dark:border-slate-800',
          'hover:bg-slate-200/60 dark:hover:bg-slate-800/60 focus:outline-none focus:bg-slate-200/60 dark:focus:bg-slate-800/60',
          'transition-colors duration-75 cursor-pointer',
          'sticky top-0 z-10',
        ].join(' ')}
        onClick={onToggle}
        aria-expanded={isExpanded}
        aria-label={`${label} — ${count} flows`}
        data-testid={`flow-group-header-${kind}`}
      >
        {/* Caret */}
        <ChevronRight
          className={[
            'w-3.5 h-3.5 text-slate-400 dark:text-slate-500 flex-shrink-0',
            'transition-transform duration-150',
            isExpanded ? 'rotate-90' : '',
          ].join(' ')}
          aria-hidden
        />

        {/* Kind icon */}
        <Icon
          className={`w-4 h-4 flex-shrink-0 ${accentClass}`}
          aria-hidden
        />

        {/* Label */}
        <span className="flex-1 min-w-0 text-left text-sm font-medium text-slate-700 dark:text-slate-300 truncate">
          {label}
        </span>

        {/* Count badge */}
        <span
          className="flex-shrink-0 text-xs tabular-nums text-slate-400 dark:text-slate-400 bg-slate-200 dark:bg-slate-800 border border-slate-300 dark:border-slate-700 rounded px-1.5 py-0.5 ml-1"
          title={`${count} flows`}
          aria-label={`${count} flows`}
        >
          {count}
        </span>
      </button>

      {/* Flow rows */}
      {isExpanded && (
        <div role="rowgroup" data-testid={`flow-group-rows-${kind}`}>
          {children}
        </div>
      )}
    </div>
  )
}
