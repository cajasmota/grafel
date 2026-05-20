import { Search, RotateCcw, Camera, Maximize2, Pause, Play, ZoomIn, ZoomOut, RefreshCw } from 'lucide-react'
import { useRef } from 'react'
import { useGraphCameraStore } from '@/store/graphCameraStore'

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
  /** Cross-repo edge filter toggle (Task 3) */
  crossRepoOnly: boolean
  onCrossRepoOnlyChange: (v: boolean) => void
  className?: string
}

const ZOOM_STEP = 1.4

const btnCls = [
  'p-1.5 rounded transition-colors',
  'text-slate-400 dark:text-slate-400',
  'hover:text-slate-800 dark:hover:text-slate-200',
  'hover:bg-slate-200 dark:hover:bg-slate-800',
  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
].join(' ')

/**
 * Graph toolbar: search input, zoom controls, view controls, cross-repo toggle, reset view, save snapshot.
 *
 * Layout toggles removed in #1023 (Tree was broken; 3D dropped with react-force-graph).
 * Cosmograph renders a single 2D GPU force layout at 60fps.
 *
 * Features:
 * - Zoom controls (in/out/fit/reset) and cross-repo-only edge filter (#1074)
 * - Fit View, Reset Zoom, Pause/Resume simulation buttons (#1070)
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
  crossRepoOnly,
  onCrossRepoOnlyChange,
  className = '',
}: GraphToolbarProps) {
  const searchRef = useRef<HTMLInputElement>(null)
  const { graphRef } = useGraphCameraStore()

  function handleZoomIn() {
    if (!graphRef) return
    const current = graphRef.getZoomLevel?.() ?? 1
    graphRef.setZoomLevel(current * ZOOM_STEP, 200)
  }

  function handleZoomOut() {
    if (!graphRef) return
    const current = graphRef.getZoomLevel?.() ?? 1
    graphRef.setZoomLevel(current / ZOOM_STEP, 200)
  }

  function handleFitView() {
    graphRef?.fitView(400)
  }

  function handleResetZoom() {
    if (!graphRef) return
    graphRef.setZoomLevel(1.0, 300)
    setTimeout(() => graphRef.fitView(300), 350)
  }

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

      {/* Cross-repo only toggle — Task 3 (#1074) */}
      <button
        type="button"
        onClick={() => onCrossRepoOnlyChange(!crossRepoOnly)}
        aria-pressed={crossRepoOnly}
        title={crossRepoOnly ? 'Show all edges' : 'Show only cross-repo edges'}
        className={[
          'flex items-center gap-1.5 px-2 py-1 rounded text-[11px] font-medium border transition-colors',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
          crossRepoOnly
            ? 'bg-sky-500/20 text-sky-400 border-sky-500/50'
            : 'bg-transparent text-slate-400 border-slate-600 hover:border-slate-500 hover:text-slate-300',
        ].join(' ')}
        data-testid="cross-repo-toggle"
      >
        <span
          className={[
            'w-1.5 h-1.5 rounded-full',
            crossRepoOnly ? 'bg-sky-400' : 'bg-slate-500',
          ].join(' ')}
          aria-hidden
        />
        cross-repo
      </button>

      {/* Zoom control group — Task 1 (#1074) */}
      <div
        className="flex items-center divide-x divide-slate-300 dark:divide-slate-700 rounded border border-slate-300 dark:border-slate-700 overflow-hidden bg-slate-100/70 dark:bg-slate-900/70 backdrop-blur-sm"
        role="group"
        aria-label="Zoom controls"
      >
        <button
          type="button"
          onClick={handleZoomIn}
          title="Zoom in"
          aria-label="Zoom in"
          className={btnCls}
          data-testid="zoom-in"
        >
          <ZoomIn className="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={handleZoomOut}
          title="Zoom out"
          aria-label="Zoom out"
          className={btnCls}
          data-testid="zoom-out"
        >
          <ZoomOut className="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={handleFitView}
          title="Fit view"
          aria-label="Fit all nodes in view"
          className={btnCls}
          data-testid="zoom-fit"
        >
          <Maximize2 className="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={handleResetZoom}
          title="Reset zoom to 1×"
          aria-label="Reset zoom to 1×"
          className={btnCls}
          data-testid="zoom-reset"
        >
          <RefreshCw className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Pause / resume simulation — #1070 */}
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

      {/* Reset view (camera + fit) */}
      <button
        type="button"
        onClick={onResetView}
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
