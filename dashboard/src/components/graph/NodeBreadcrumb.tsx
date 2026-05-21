/**
 * NodeBreadcrumb — compact trail of recently-keyboard-navigated nodes.
 *
 * Closes #1368 (beyond-the-minimum: breadcrumb of recently-visited nodes).
 *
 * Shows the last N visited node ids as pill chips.  The current position in
 * the history stack is highlighted.  Clicking a chip jumps directly to that
 * node (calls onSelectNode).
 *
 * Hidden when history has 0 or 1 entries.
 */

import { memo } from 'react'
import type { GraphNode } from '@/types/api'

export interface NodeBreadcrumbProps {
  /** Full node list — used to resolve labels from ids */
  nodes: GraphNode[]
  /** History of visited node ids (newest last) */
  history: readonly string[]
  /** Current position in history */
  historyIdx: number
  /** Jump to a specific node by id */
  onSelectNode: (id: string) => void
  /** Max chips to show (older entries are clipped) */
  maxVisible?: number
}

const MAX_VISIBLE_DEFAULT = 7

export const NodeBreadcrumb = memo(function NodeBreadcrumb({
  nodes,
  history,
  historyIdx,
  onSelectNode,
  maxVisible = MAX_VISIBLE_DEFAULT,
}: NodeBreadcrumbProps) {
  if (history.length <= 1) return null

  // Show only the last maxVisible entries (the most recent section of the trail)
  const start = Math.max(0, history.length - maxVisible)
  const visible = history.slice(start)

  // Adjusted index for the visible window
  const visibleCurrentIdx = historyIdx - start

  function labelFor(id: string): string {
    const node = nodes.find((n) => String(n.id) === id)
    if (!node) return id
    const lbl = node.label ?? id
    // Truncate to ~22 chars
    return lbl.length > 22 ? lbl.slice(0, 20) + '…' : lbl
  }

  return (
    <nav
      aria-label="Node navigation breadcrumb"
      className="flex items-center gap-1 px-3 py-1 overflow-x-auto scrollbar-hide"
      data-testid="node-breadcrumb"
    >
      {start > 0 && (
        <span
          className="text-[10px] text-slate-500 dark:text-slate-600 font-mono flex-shrink-0"
          aria-hidden
        >
          …
        </span>
      )}
      {visible.map((id, i) => {
        const isCurrent = i === visibleCurrentIdx
        const label = labelFor(id)
        return (
          <button
            key={`${id}-${start + i}`}
            type="button"
            onClick={() => onSelectNode(id)}
            aria-current={isCurrent ? 'location' : undefined}
            title={id}
            className={[
              'flex-shrink-0 px-2 py-0.5 rounded-full text-[10px] font-medium transition-colors truncate max-w-[10rem]',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              isCurrent
                ? 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
                : 'bg-slate-200/60 dark:bg-slate-800/60 text-slate-500 dark:text-slate-400 border border-transparent hover:bg-slate-300/60 dark:hover:bg-slate-700/60',
            ].join(' ')}
            data-testid={`breadcrumb-node-${start + i}`}
          >
            {label}
          </button>
        )
      })}
    </nav>
  )
})
