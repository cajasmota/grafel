import { useEffect, useRef } from 'react'
import { Loader2 } from 'lucide-react'
import { NodeChip } from './NodeChip'
import type { GraphNode } from '@/types/api'

interface GraphSearchTypeaheadProps {
  results: GraphNode[]
  isSearching: boolean
  onSelect: (node: GraphNode) => void
  onClose: () => void
  /** aria-controls the search input that triggered this */
  inputId?: string
}

/**
 * Floating dropdown that shows search results.
 * Selects and zooms on pick. Closed on Escape.
 */
export function GraphSearchTypeahead({
  results,
  isSearching,
  onSelect,
  onClose,
  inputId,
}: GraphSearchTypeaheadProps) {
  const listRef = useRef<HTMLUListElement>(null)

  // Close on Escape
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  if (results.length === 0 && !isSearching) return null

  return (
    <div
      className={[
        'absolute top-full left-0 right-0 z-50 mt-1',
        'rounded-lg border border-slate-300 dark:border-slate-700 bg-slate-100 dark:bg-slate-900 shadow-xl',
        'max-h-72 overflow-y-auto',
      ].join(' ')}
      role="listbox"
      id={inputId ? `${inputId}-results` : undefined}
      aria-label="Search results"
    >
      {isSearching && (
        <div className="flex items-center gap-2 px-3 py-2 text-xs text-slate-400 dark:text-slate-500">
          <Loader2 className="w-3 h-3 animate-spin" />
          Searching…
        </div>
      )}
      <ul ref={listRef}>
        {results.map((node) => (
          <li key={node.id}>
            <button
              type="button"
              role="option"
              aria-selected={false}
              onClick={() => {
                onSelect(node)
                onClose()
              }}
              className={[
                'w-full flex items-center gap-2 px-3 py-2 text-left',
                'hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors',
                'focus-visible:outline-none focus-visible:bg-slate-200 dark:focus-visible:bg-slate-800',
              ].join(' ')}
            >
              <NodeChip kind={node.kind} label={node.label} repo={node.repo} />
              <span className="ml-auto text-[10px] text-slate-500 dark:text-slate-600 truncate max-w-[100px]">
                {node.repo}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
