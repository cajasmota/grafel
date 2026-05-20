import { Search, RotateCcw, Camera } from 'lucide-react'
import { useRef } from 'react'

// #1023: layout modes collapsed to 'force' only — Cosmograph 2D GPU force is the single renderer.
// Tree layout removed (was broken with cyclic graphs; dagMode not supported by Cosmograph).
// 3D toggle removed (3d-force-graph dependency dropped).
export type LayoutMode = 'force'

interface GraphToolbarProps {
  searchQuery: string
  onSearchChange: (q: string) => void
  onResetView: () => void
  onSaveSnapshot: () => void
  layoutMode: LayoutMode
  onLayoutChange: (mode: LayoutMode) => void
  className?: string
}

/**
 * Graph toolbar: search input, reset view, save snapshot.
 *
 * Layout toggles removed in #1023 (Tree was broken; 3D dropped with react-force-graph).
 * Cosmograph renders a single 2D GPU force layout at 60fps.
 */
export function GraphToolbar({
  searchQuery,
  onSearchChange,
  onResetView,
  onSaveSnapshot,
  className = '',
}: Omit<GraphToolbarProps, 'layoutMode' | 'onLayoutChange'> & {
  layoutMode?: LayoutMode
  onLayoutChange?: (mode: LayoutMode) => void
}) {
  const searchRef = useRef<HTMLInputElement>(null)

  return (
    <div
      className={[
        'flex items-center gap-2 px-3 py-2',
        'bg-slate-100/80 dark:bg-slate-900/80 backdrop-blur-sm border-b border-slate-200 dark:border-slate-800',
        className,
      ].join(' ')}
      role="toolbar"
      aria-label="Graph controls"
    >
      {/* Search */}
      <div className="relative flex-1 max-w-xs">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 dark:text-slate-500" aria-hidden />
        <input
          ref={searchRef}
          id="graph-search"
          type="search"
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder="Search nodes… (/)"
          className={[
            'w-full pl-8 pr-3 py-1.5 rounded text-sm',
            'bg-slate-200 dark:bg-slate-800 text-slate-800 dark:text-slate-200 placeholder-slate-500',
            'border border-slate-300 dark:border-slate-700 focus:border-sky-600 focus:outline-none focus:ring-1 focus:ring-sky-600',
          ].join(' ')}
          aria-label="Search graph nodes"
          aria-controls="graph-search-results"
          autoComplete="off"
          spellCheck={false}
        />
      </div>

      <div className="flex-1" aria-hidden />

      {/* Reset view */}
      <button
        type="button"
        onClick={onResetView}
        title="Reset view"
        className="p-1.5 rounded text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
        aria-label="Reset camera view"
      >
        <RotateCcw className="w-4 h-4" />
      </button>

      {/* Save snapshot */}
      <button
        type="button"
        onClick={onSaveSnapshot}
        title="Save snapshot"
        className="p-1.5 rounded text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
        aria-label="Save graph snapshot as PNG"
      >
        <Camera className="w-4 h-4" />
      </button>
    </div>
  )
}
