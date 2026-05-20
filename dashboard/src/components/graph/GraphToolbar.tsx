import { Search, RotateCcw, Camera, Box, Grid2x2, GitBranch, Globe } from 'lucide-react'
import { useRef } from 'react'

export type LayoutMode = 'force' | '2d' | 'tree' | 'sphere'

interface GraphToolbarProps {
  searchQuery: string
  onSearchChange: (q: string) => void
  onResetView: () => void
  onSaveSnapshot: () => void
  layoutMode: LayoutMode
  onLayoutChange: (mode: LayoutMode) => void
  className?: string
}

const LAYOUT_BUTTONS: { mode: LayoutMode; icon: React.FC<{ className?: string }>; label: string }[] = [
  { mode: 'force', icon: Box, label: '3D force' },
  { mode: '2d', icon: Grid2x2, label: '2D force' },
  { mode: 'tree', icon: GitBranch, label: 'Tree' },
  { mode: 'sphere', icon: Globe, label: 'Globe' },
]

/**
 * Graph toolbar: search input, layout toggles (3D|2D|tree|sphere), reset view, save snapshot.
 */
export function GraphToolbar({
  searchQuery,
  onSearchChange,
  onResetView,
  onSaveSnapshot,
  layoutMode,
  onLayoutChange,
  className = '',
}: GraphToolbarProps) {
  const searchRef = useRef<HTMLInputElement>(null)

  // Keyboard: "/" focuses search from anywhere on the page
  // (registered in the route via useEffect)
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

      {/* Layout toggles */}
      <div className="flex items-center gap-0.5" role="group" aria-label="Graph layout">
        {LAYOUT_BUTTONS.map(({ mode, icon: Icon, label }) => (
          <button
            key={mode}
            type="button"
            onClick={() => onLayoutChange(mode)}
            aria-pressed={layoutMode === mode}
            className={[
              'flex items-center gap-1 px-2 py-1.5 rounded text-xs font-medium transition-colors',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              layoutMode === mode
                ? 'bg-sky-700/60 text-sky-200 border border-sky-700'
                : 'text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 border border-transparent',
            ].join(' ')}
            title={label}
            aria-label={`Switch to ${label} layout`}
          >
            <Icon className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">{label}</span>
          </button>
        ))}
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
