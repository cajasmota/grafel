import { useState, useCallback, useEffect, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useGraphData } from '@/hooks/graph/useGraphData'
import { useGraphSelection } from '@/hooks/graph/useGraphSelection'
import { useEdgeKindFilters } from '@/hooks/graph/useEdgeKindFilters'
import { useEntityInspector } from '@/hooks/graph/useEntityInspector'
import { useCommunityColors } from '@/hooks/graph/useCommunityColors'
import { useGraphSearch } from '@/hooks/graph/useGraphSearch'
import { useGraphCameraStore } from '@/store/graphCameraStore'
import { GraphCanvas3D } from '@/components/graph/GraphCanvas3D'
import { GraphCanvas2D } from '@/components/graph/GraphCanvas2D'
import { GraphToolbar } from '@/components/graph/GraphToolbar'
import { EdgeKindFilters } from '@/components/graph/EdgeKindFilters'
import { CommunityLegend } from '@/components/graph/CommunityLegend'
import { EntityInspector } from '@/components/graph/EntityInspector'
import { GraphSearchTypeahead } from '@/components/graph/GraphSearchTypeahead'
import { LodLevelIndicator } from '@/components/graph/LodLevelIndicator'
import {
  GraphEmptyState,
  GraphLoadingState,
  GraphErrorState,
} from '@/components/graph/GraphEmptyState'
import type { GraphNode, RelationshipKind } from '@/types/api'
import type { LayoutMode } from '@/components/graph/GraphToolbar'

/**
 * Surface 1 — Graph Viewer
 *
 * URL params:
 *   ?lod=           LoD override (zoom-out|mid|zoom-in)
 *   ?filter_kind=   comma-separated RelationshipKind values
 *   ?filter_repo=   repo slug
 *   ?selected=      selected entity ID (shareable deep-link)
 */
export function GraphRoute() {
  const { group } = useParams<{ group: string }>()
  const navigate = useNavigate()

  // ── URL state ──────────────────────────────────────────────────────────────
  const { selectedNodeId, select: selectNode, clear: clearSelection } = useGraphSelection()
  const { activeKinds, toggle: toggleKind, clearAll: clearKindFilters } = useEdgeKindFilters()

  // ── Camera / zoom ──────────────────────────────────────────────────────────
  const { zoomLevel, hoveredNodeId, setHoveredNode, zoomToNode, resetView } = useGraphCameraStore()

  // ── Layout / view state ────────────────────────────────────────────────────
  const [layoutMode, setLayoutMode] = useState<LayoutMode>('force')
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearchResults, setShowSearchResults] = useState(false)
  const [highContrast, setHighContrast] = useState(false)
  const [hoveredCommunityId, setHoveredCommunityId] = useState<number | null>(null)
  const searchContainerRef = useRef<HTMLDivElement>(null)

  // Respect prefers-reduced-motion → default to 2D
  const [use2D, setUse2D] = useState(() =>
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  )
  const [preferHighContrast] = useState(() =>
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-contrast: more)').matches,
  )
  useEffect(() => {
    if (preferHighContrast) setHighContrast(true)
  }, [preferHighContrast])

  // ── Data ───────────────────────────────────────────────────────────────────
  const { nodes, edges, communities, allEdgeKinds, lodLevel, totalNodeCount, isLoading, error, refetch } =
    useGraphData(
      group ?? '',
      { edge_kinds: activeKinds.size > 0 ? [...activeKinds] as RelationshipKind[] : undefined },
      zoomLevel,
      null,
      selectedNodeId,
    )

  const colorMap = useCommunityColors(communities)

  // ── Inspector ──────────────────────────────────────────────────────────────
  const { data: inspectorData, isLoading: inspectorLoading } = useEntityInspector(
    group ?? '',
    selectedNodeId,
  )

  // ── Search ─────────────────────────────────────────────────────────────────
  const { results: searchResults, isSearching } = useGraphSearch(searchQuery, nodes)
  useEffect(() => {
    setShowSearchResults(searchQuery.length > 0)
  }, [searchQuery])

  // ── Keyboard shortcuts ─────────────────────────────────────────────────────
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      // "/" focuses search
      if (e.key === '/' && !isInputActive()) {
        e.preventDefault()
        document.getElementById('graph-search')?.focus()
      }
      // Esc closes inspector
      if (e.key === 'Escape') {
        if (showSearchResults) {
          setShowSearchResults(false)
          setSearchQuery('')
        } else {
          clearSelection()
        }
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [clearSelection, showSearchResults])

  // ── Handlers ───────────────────────────────────────────────────────────────
  const handleNodeClick = useCallback((node: GraphNode) => {
    selectNode(node.id)
  }, [selectNode])

  const handleNodeHover = useCallback((node: GraphNode | null) => {
    setHoveredNode(node?.id ?? null)
  }, [setHoveredNode])

  const handleSearchSelect = useCallback((node: GraphNode) => {
    selectNode(node.id)
    zoomToNode(node.id)
    setSearchQuery('')
    setShowSearchResults(false)
  }, [selectNode, zoomToNode])

  const handleSaveSnapshot = useCallback(() => {
    // Extract canvas and save as PNG
    const canvas = document.querySelector<HTMLCanvasElement>('.graph-canvas canvas')
    if (!canvas) return
    const a = document.createElement('a')
    a.href = canvas.toDataURL('image/png')
    a.download = `archigraph-${group ?? 'graph'}-${Date.now()}.png`
    a.click()
  }, [group])

  const handleLayoutChange = useCallback((mode: LayoutMode) => {
    setLayoutMode(mode)
    setUse2D(mode === '2d')
  }, [])

  const handleOpenInFlows = useCallback((entityId: string) => {
    navigate(`/${group}/flows?entry=${encodeURIComponent(entityId)}`)
  }, [group, navigate])

  // ── Render ─────────────────────────────────────────────────────────────────
  if (!group) {
    return (
      <div className="h-full flex items-center justify-center">
        <GraphEmptyState reason="no-group" />
      </div>
    )
  }

  const showInspector = !!selectedNodeId
  const canvasReady = !isLoading && !error && lodLevel !== 'blocked'
  const isEmpty = !isLoading && !error && nodes.length === 0

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Toolbar */}
      <div ref={searchContainerRef} className="relative">
        <GraphToolbar
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
          onResetView={resetView}
          onSaveSnapshot={handleSaveSnapshot}
          layoutMode={layoutMode}
          onLayoutChange={handleLayoutChange}
        />
        {showSearchResults && (
          <div className="absolute left-3 right-3 top-full z-50">
            <GraphSearchTypeahead
              results={searchResults}
              isSearching={isSearching}
              onSelect={handleSearchSelect}
              onClose={() => setShowSearchResults(false)}
              inputId="graph-search"
            />
          </div>
        )}
      </div>

      {/* Edge kind filters */}
      {allEdgeKinds.length > 0 && (
        <div className="px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 overflow-x-auto">
          <EdgeKindFilters
            kinds={allEdgeKinds}
            activeKinds={activeKinds}
            onToggle={toggleKind}
            onClear={clearKindFilters}
          />
        </div>
      )}

      {/* Main area */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left: community legend */}
        <aside
          className="hidden lg:flex flex-col w-48 min-w-[160px] border-r border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 p-2"
          aria-label="Community legend sidebar"
        >
          <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold px-2 pb-1">
            Communities
          </p>
          <CommunityLegend
            communities={communities}
            colorMap={colorMap}
            highlightId={hoveredCommunityId}
            onHover={setHoveredCommunityId}
          />
        </aside>

        {/* Center: canvas */}
        <div className="relative flex-1 min-w-0 graph-canvas">
          {isLoading && <GraphLoadingState />}

          {!isLoading && error && (
            <GraphErrorState
              message={error.message}
              onRetry={refetch}
            />
          )}

          {!isLoading && !error && lodLevel === 'blocked' && (
            <GraphEmptyState reason="blocked" />
          )}

          {!isLoading && !error && isEmpty && lodLevel !== 'blocked' && (
            <GraphEmptyState reason="filtered" />
          )}

          {canvasReady && !isEmpty && (
            use2D ? (
              <GraphCanvas2D
                nodes={nodes}
                edges={edges}
                selectedNodeId={selectedNodeId}
                hoveredNodeId={hoveredNodeId}
                onNodeClick={handleNodeClick}
                onNodeHover={handleNodeHover}
                onZoomChange={useGraphCameraStore.getState().setZoomLevel}
                highContrast={highContrast}
                className="w-full h-full"
              />
            ) : (
              <GraphCanvas3D
                nodes={nodes}
                edges={edges}
                selectedNodeId={selectedNodeId}
                hoveredNodeId={hoveredNodeId}
                onNodeClick={handleNodeClick}
                onNodeHover={handleNodeHover}
                onZoomChange={useGraphCameraStore.getState().setZoomLevel}
                highContrast={highContrast}
                className="w-full h-full"
              />
            )
          )}

          {/* LoD indicator (top-right overlay) */}
          {!isLoading && !error && (
            <div className="absolute top-3 right-3 z-10 flex flex-col items-end gap-2">
              <LodLevelIndicator
                lodLevel={lodLevel}
                visibleCount={nodes.length}
                totalCount={totalNodeCount}
              />
              {/* High-contrast toggle */}
              <button
                type="button"
                onClick={() => setHighContrast((v) => !v)}
                className={[
                  'text-[10px] px-2 py-0.5 rounded border transition-colors',
                  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                  highContrast
                    ? 'bg-slate-200 text-slate-900 border-slate-300'
                    : 'bg-slate-200/80 dark:bg-slate-800/80 text-slate-400 dark:text-slate-400 border-slate-300 dark:border-slate-700',
                ].join(' ')}
                aria-pressed={highContrast}
                aria-label="Toggle high-contrast mode"
                title="Toggle high-contrast mode"
              >
                HC
              </button>
            </div>
          )}
        </div>

        {/* Right: entity inspector */}
        {showInspector && (
          <EntityInspector
            data={inspectorData}
            isLoading={inspectorLoading}
            onClose={clearSelection}
            onSelectEntity={(id) => {
              selectNode(id)
              zoomToNode(id)
            }}
            onOpenInFlows={handleOpenInFlows}
          />
        )}
      </div>
    </div>
  )
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function isInputActive(): boolean {
  const el = document.activeElement
  return el instanceof HTMLInputElement ||
    el instanceof HTMLTextAreaElement ||
    (el instanceof HTMLElement && el.isContentEditable)
}
