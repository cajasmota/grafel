/**
 * PathsGroup — Collapsible group header for the Paths grouped view.
 *
 * Shows:
 *  - Caret (left) for expand/collapse
 *  - Controller / module name
 *  - Stacked mini-bar with HTTP method distribution
 *  - Total endpoint count badge (right)
 *
 * Children (PathRow list) are rendered when expanded.
 */

import { ChevronRight } from 'lucide-react'
import type { HttpVerb } from '@/types/api'
import type { PathGroup } from '@/lib/groupPaths'

// Tailwind bg classes that match the app's verb colour palette (dark-mode)
const VERB_BAR_COLORS: Partial<Record<HttpVerb, string>> = {
  GET:     'bg-emerald-500',
  POST:    'bg-blue-500',
  PUT:     'bg-orange-500',
  PATCH:   'bg-yellow-500',
  DELETE:  'bg-red-500',
  HEAD:    'bg-slate-500',
  OPTIONS: 'bg-slate-500',
  ANY:     'bg-slate-400',
  WS:      'bg-purple-500',
}

interface MethodBarProps {
  verbCounts: PathGroup['verbCounts']
}

/** Mini stacked bar showing proportional HTTP method distribution */
function MethodBar({ verbCounts }: MethodBarProps) {
  const total = verbCounts.reduce((s, vc) => s + vc.count, 0)
  if (total === 0) return null

  return (
    <div
      className="flex h-2 w-24 rounded-full overflow-hidden gap-px"
      role="img"
      aria-label={verbCounts.map((vc) => `${vc.verb}: ${vc.count}`).join(', ')}
    >
      {verbCounts.map(({ verb, count }) => (
        <div
          key={verb}
          className={`${VERB_BAR_COLORS[verb] ?? 'bg-slate-500'} opacity-80`}
          style={{ flex: count / total }}
          title={`${verb}: ${count}`}
        />
      ))}
    </div>
  )
}

interface PathsGroupProps {
  group: PathGroup
  isExpanded: boolean
  onToggle: () => void
  children: React.ReactNode
}

export function PathsGroup({ group, isExpanded, onToggle, children }: PathsGroupProps) {
  return (
    <div>
      {/* Group header */}
      <button
        type="button"
        className={[
          'w-full flex items-center gap-2 px-3 py-2',
          'bg-slate-900/80 border-b border-slate-800',
          'hover:bg-slate-800/60 focus:outline-none focus:bg-slate-800/60',
          'transition-colors duration-75 cursor-pointer',
          'sticky top-0 z-10',
        ].join(' ')}
        onClick={onToggle}
        aria-expanded={isExpanded}
        aria-label={`${group.name} — ${group.paths.length} paths`}
      >
        {/* Caret */}
        <ChevronRight
          className={[
            'w-3.5 h-3.5 text-slate-500 flex-shrink-0',
            'transition-transform duration-150',
            isExpanded ? 'rotate-90' : '',
          ].join(' ')}
          aria-hidden
        />

        {/* Controller / module name */}
        <span className="flex-1 min-w-0 text-left text-base font-medium text-slate-200 truncate">
          {group.name}
        </span>

        {/* Mini method distribution bar */}
        <MethodBar verbCounts={group.verbCounts} />

        {/* Total count badge */}
        <span
          className="flex-shrink-0 text-xs tabular-nums text-slate-400 bg-slate-800 border border-slate-700 rounded px-1.5 py-0.5 ml-1"
          title={`${group.totalEndpoints} endpoints`}
        >
          {group.paths.length}
        </span>
      </button>

      {/* Path rows */}
      {isExpanded && (
        <div role="rowgroup">
          {children}
        </div>
      )}
    </div>
  )
}
