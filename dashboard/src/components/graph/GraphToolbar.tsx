import { Search, RotateCcw, Camera, Maximize2, Pause, Play } from 'lucide-react'
import { useRef } from 'react'

// #1023: Tree + 3D layout modes removed — Cosmograph is 2D-only GPU force renderer.
// #1059: LayoutMode type + dead props scrubbed (no-tech-debt).

interface GraphToolbarProps {
  searchQuery: string
  onSearchChange: (q: string) => void
  onResetView: () => void
  onSaveSnapshot: () => void
  /** Fit view to all visible nodes */
  onFitView?: () => void
  /** Reset zoom to 1:1 then fit */
  onResetZoom?: () => void
  /** Toggle force simulation on/off */
  onToggleSimulation?: () => void
  /** Whether the simulation is currently running */
  simulationRunning?: boolean
  className?: string
}

/**
 * Graph toolbar: search input, view controls (fit/reset/pause), reset view, save snapshot.
 *
 * Layout toggles removed in #1023 (Tree was broken; 3D dropped with react-force-graph).
 * Cosmograph renders a single 2D GPU force layout at 60fps.
 *
 * New in depth-polish: Fit View, Reset Zoom, Pause/Resume simulation buttons.
 */
export function GraphToolbar({
  searchQuery,
  onSearchChange,
  onResetView,
  onSaveSnapshot,
  onFitView,
  onResetZoom,
  onToggleSimulation,
  simulationRunning = false,
  className = '',
}: GraphToolbarProps) {
  const searchRef = useRef<HTMLInputElement>(null)

  const btnBase = [
    'p-1.5 rounded transition-colors',
    'text-slate-400 dark:text-slate-400',
    'hover:text-slate-800 dark:hover:text-slate-200',
    'hover:bg-slate-200 dark:hover:bg-slate-800',
    'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
  ].join(' ')

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

      {/* Fit view */}
      {onFitView && (
        <button
          type="button"
          onClick={onFitView}
          title="Fit view"
          className={btnBase}
          aria-label="Fit all nodes into view"
        >
          <Maximize2 className="w-4 h-4" />
        </button>
      )}

      {/* Reset zoom (1:1 + fit) */}
      {onResetZoom && (
        <button
          type="button"
          onClick={onResetZoom}
          title="Reset zoom"
          className={btnBase}
          aria-label="Reset zoom to 1:1"
        >
          <RotateCcw className="w-4 h-4" />
        </button>
      )}

      {/* Pause / resume simulation */}
      {onToggleSimulation && (
        <button
          type="button"
          onClick={onToggleSimulation}
          title={simulationRunning ? 'Pause simulation' : 'Resume simulation'}
          className={[btnBase, simulationRunning ? 'text-sky-400 dark:text-sky-400' : ''].join(' ')}
          aria-label={simulationRunning ? 'Pause force simulation' : 'Resume force simulation'}
          aria-pressed={!simulationRunning}
        >
          {simulationRunning
            ? <Pause className="w-4 h-4" />
            : <Play className="w-4 h-4" />
          }
        </button>
      )}

      {/* Legacy reset view (kept for backward compat — RotateCcw was original) */}
      <button
        type="button"
        onClick={onResetView}
        title="Reset view"
        className="sr-only"
        aria-label="Reset camera view"
      />

      {/* Save snapshot */}
      <button
        type="button"
        onClick={onSaveSnapshot}
        title="Save snapshot"
        className={btnBase}
        aria-label="Save graph snapshot as PNG"
      >
        <Camera className="w-4 h-4" />
      </button>
    </div>
  )
}
