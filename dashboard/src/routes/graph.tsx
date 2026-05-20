import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useGraphData } from '@/hooks/graph/useGraphData'
import { useGraphSelection } from '@/hooks/graph/useGraphSelection'
import { useEdgeKindFilters } from '@/hooks/graph/useEdgeKindFilters'
import { useEntityInspector } from '@/hooks/graph/useEntityInspector'
import { useCommunityColors } from '@/hooks/graph/useCommunityColors'
import { useGraphSearch } from '@/hooks/graph/useGraphSearch'
import { useGraphCameraStore, useSimulationRunning } from '@/store/graphCameraStore'
import { useThemeContext } from '@/context/ThemeContext'
import { GraphCanvas } from '@/components/graph/GraphCanvas'
import { GraphToolbar } from '@/components/graph/GraphToolbar'
import { EdgeKindFilters } from '@/components/graph/EdgeKindFilters'
import { CommunityLegend } from '@/components/graph/CommunityLegend'
import { EntityInspector } from '@/components/graph/EntityInspector'
import { GraphSearchTypeahead } from '@/components/graph/GraphSearchTypeahead'
import {
  GraphEmptyState,
  GraphLoadingState,
  GraphErrorState,
} from '@/components/graph/GraphEmptyState'
import { repoColor } from '@/lib/colors'
import type { GraphNode, RelationshipKind } from '@/types/api'

/**
 * Surface 1 — Graph Viewer
 *
 * #1023: migrated to Cosmograph (GPU WebGL). LoD architecture removed.
 * Single canvas, dense tier always, no zoom-level switching.
 *
 * URL params:
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

  // ── Theme ──────────────────────────────────────────────────────────────────
  const { isDark } = useThemeContext()

  // ── Camera state ───────────────────────────────────────────────────────────
  const { hoveredNodeId, setHoveredNode, zoomToNode, resetView, fitView, resetZoom, toggleSimulation } = useGraphCameraStore()
  const simulationRunning = useSimulationRunning()

  // ── View state ─────────────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearchResults, setShowSearchResults] = useState(false)
  const [highContrast, setHighContrast] = useState(false)
  const [hoveredCommunityId, setHoveredCommunityId] = useState<number | null>(null)

  // ── Hover tooltip state (#1060) ────────────────────────────────────────────
  // Track cursor position in screen coordinates for tooltip placement
  const [cursorPos, setCursorPos] = useState<{ x: number; y: number } | null>(null)
  const searchContainerRef = useRef<HTMLDivElement>(null)

  // ── Community drill-in state ───────────────────────────────────────────────
  const [selectedCommunityId, setSelectedCommunityId] = useState<number | null>(null)
  const [selectedCommunityName, setSelectedCommunityName] = useState<string | null>(null)

  // ── Repo filter state ──────────────────────────────────────────────────────
  // null = not yet initialised (wait for communities to load); Set = active slugs
  const [activeRepos, setActiveRepos] = useState<Set<string> | null>(null)
  const [allRepoSlugs, setAllRepoSlugs] = useState<string[]>([])

  // Respect prefers-contrast
  const [preferHighContrast] = useState(() =>
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-contrast: more)').matches,
  )
  useEffect(() => {
    if (preferHighContrast) setHighContrast(true)
  }, [preferHighContrast])

  // ── Data ───────────────────────────────────────────────────────────────────
  const effectiveActiveRepos = activeRepos && allRepoSlugs.length > 0
    ? (activeRepos.size === allRepoSlugs.length ? null : activeRepos)
    : null

  const { nodes, edges, communities, allEdgeKinds, totalNodeCount, isLoading, error, refetch } =
    useGraphData(
      group ?? '',
      { edge_kinds: activeKinds.size > 0 ? [...activeKinds] as RelationshipKind[] : undefined },
      1.0,  // zoomLevel: compat param — no longer drives LoD
      null, // viewport: compat param — no longer used
      selectedNodeId,
      selectedCommunityId,
      effectiveActiveRepos,
    )

  const colorMap = useCommunityColors(communities)

  // ── Hover neighbor counts (#1060) ──────────────────────────────────────────
  // Pre-index edges by source and target so neighbor lookup is O(1) per query.
  const edgeIndex = useMemo(() => {
    const callers = new Map<string, number>()  // target → inbound count
    const callees = new Map<string, number>()  // source → outbound count
    for (const e of edges) {
      const src = String(e.source)
      const tgt = String(e.target)
      callees.set(src, (callees.get(src) ?? 0) + 1)
      callers.set(tgt, (callers.get(tgt) ?? 0) + 1)
    }
    return { callers, callees }
  }, [edges])

  // Derive unique repo slugs from communities once data loads
  useEffect(() => {
    if (communities.length === 0) return
    const slugs = [...new Set(communities.map((c) => c.repo))].sort()
    setAllRepoSlugs((prev) => {
      if (prev.join(',') === slugs.join(',')) return prev
      return slugs
    })
    // Initialise activeRepos to all repos (no filter) on first load
    setActiveRepos((prev) => {
      if (prev !== null) return prev // already initialised
      return new Set(slugs)
    })
  }, [communities])

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
      if (e.key === '/' && !isInputActive()) {
        e.preventDefault()
        document.getElementById('graph-search')?.focus()
      }
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
    if (!node) setCursorPos(null)
  }, [setHoveredNode])

  const handleCursorMove = useCallback((x: number, y: number) => {
    if (hoveredNodeId) setCursorPos({ x, y })
  }, [hoveredNodeId])

  const handleSearchSelect = useCallback((node: GraphNode) => {
    selectNode(node.id)
    zoomToNode(node.id)
    setSearchQuery('')
    setShowSearchResults(false)
  }, [selectNode, zoomToNode])

  const handleSaveSnapshot = useCallback(() => {
    const canvas = document.querySelector<HTMLCanvasElement>('.graph-canvas canvas')
    if (!canvas) return
    const a = document.createElement('a')
    a.href = canvas.toDataURL('image/png')
    a.download = `archigraph-${group ?? 'graph'}-${Date.now()}.png`
    a.click()
  }, [group])

  const handleOpenInFlows = useCallback((entityId: string) => {
    navigate(`/${group}/flows?entry=${encodeURIComponent(entityId)}`)
  }, [group, navigate])

  // ── Community drill-in handlers ────────────────────────────────────────────
  const handleCommunityClick = useCallback((id: number, name: string) => {
    setSelectedCommunityId(id)
    setSelectedCommunityName(name)
    clearSelection()
  }, [clearSelection])

  const handleClearCommunity = useCallback(() => {
    setSelectedCommunityId(null)
    setSelectedCommunityName(null)
  }, [])

  // ── Repo filter handlers ───────────────────────────────────────────────────
  const handleToggleRepo = useCallback((slug: string) => {
    setActiveRepos((prev) => {
      if (!prev) return prev
      const next = new Set(prev)
      if (next.has(slug)) {
        if (next.size <= 1) return prev // prevent deselecting all
        next.delete(slug)
      } else {
        next.add(slug)
      }
      return next
    })
  }, [])

  const handleSelectAllRepos = useCallback(() => {
    setActiveRepos(new Set(allRepoSlugs))
  }, [allRepoSlugs])

  // ── Render ─────────────────────────────────────────────────────────────────
  if (!group) {
    return (
      <div className="h-full flex items-center justify-center">
        <GraphEmptyState reason="no-group" />
      </div>
    )
  }

  const showInspector = !!selectedNodeId
  const canvasReady = !isLoading && !error
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
          onFitView={fitView}
          onResetZoom={resetZoom}
          onToggleSimulation={toggleSimulation}
          simulationRunning={simulationRunning}
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

      {/* Community drill-in breadcrumb */}
      {selectedCommunityId !== null && (
        <div className="px-3 py-1 border-b border-slate-200 dark:border-slate-800 bg-sky-950/40 flex items-center gap-1.5 text-xs text-slate-300">
          <button
            type="button"
            onClick={handleClearCommunity}
            className="text-sky-400 hover:text-sky-300 underline underline-offset-2 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
          >
            All communities
          </button>
          <span className="text-slate-500" aria-hidden>›</span>
          <span className="text-slate-200 font-medium">
            {selectedCommunityName ?? `Community ${selectedCommunityId}`}
          </span>
          <span className="text-slate-500 ml-auto">
            {nodes.length.toLocaleString()} nodes
          </span>
          <button
            type="button"
            onClick={handleClearCommunity}
            className="ml-2 px-1.5 py-0.5 rounded border border-slate-700 text-slate-400 hover:text-slate-200 hover:border-slate-500 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
            aria-label="Back to all communities"
          >
            ✕ back to all
          </button>
        </div>
      )}

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
        {/* Left: repo filter + community legend */}
        <aside
          className="hidden lg:flex flex-col w-48 min-w-[160px] border-r border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 p-2 gap-2 overflow-y-auto"
          aria-label="Graph filters sidebar"
        >
          {/* Repo filter */}
          {allRepoSlugs.length > 0 && (
            <div>
              <div className="flex items-center justify-between px-2 pb-1">
                <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold">
                  Repos
                </p>
                {activeRepos && activeRepos.size < allRepoSlugs.length && (
                  <button
                    type="button"
                    onClick={handleSelectAllRepos}
                    className="text-[9px] text-sky-500 hover:text-sky-400 focus-visible:outline-none"
                  >
                    all
                  </button>
                )}
              </div>
              <div className="flex flex-col gap-0.5">
                {allRepoSlugs.map((slug) => {
                  const active = !activeRepos || activeRepos.has(slug)
                  const color = repoColor(slug)
                  return (
                    <button
                      key={slug}
                      type="button"
                      onClick={() => handleToggleRepo(slug)}
                      className={[
                        'flex items-center gap-2 px-2 py-1 rounded text-left text-xs w-full transition-colors',
                        'hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
                        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                        active ? '' : 'opacity-40',
                      ].join(' ')}
                      aria-pressed={active}
                      title={slug}
                    >
                      <span
                        className="w-2.5 h-2.5 rounded-full shrink-0 transition-opacity"
                        style={{ background: color }}
                        aria-hidden
                      />
                      <span className="flex-1 truncate text-slate-700 dark:text-slate-300">{slug}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          )}

          {/* Divider between repos and communities */}
          {allRepoSlugs.length > 0 && (
            <div className="border-t border-slate-200 dark:border-slate-700" />
          )}

          {/* Community legend */}
          <div>
            <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold px-2 pb-1">
              Communities
            </p>
            <CommunityLegend
              communities={communities}
              colorMap={colorMap}
              highlightId={hoveredCommunityId}
              onHover={setHoveredCommunityId}
              onSelect={handleCommunityClick}
              selectedId={selectedCommunityId}
            />
          </div>
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

          {!isLoading && !error && isEmpty && (
            <GraphEmptyState reason="filtered" />
          )}

          {canvasReady && !isEmpty && (
            <GraphCanvas
              nodes={nodes}
              edges={edges}
              selectedNodeId={selectedNodeId}
              hoveredNodeId={hoveredNodeId}
              onNodeClick={handleNodeClick}
              onNodeHover={handleNodeHover}
              onCursorMove={handleCursorMove}
              highContrast={highContrast}
              isDark={isDark}
              className="w-full h-full"
            />
          )}

          {/* Hover tooltip — label + neighbor counts (#1060) */}
          {hoveredNodeId && cursorPos && (() => {
            const hovNode = nodes.find((n) => n.id === hoveredNodeId)
            if (!hovNode) return null
            const callerCount = edgeIndex.callers.get(String(hoveredNodeId)) ?? 0
            const calleeCount = edgeIndex.callees.get(String(hoveredNodeId)) ?? 0
            // Offset tooltip so it doesn't overlap the cursor
            const tx = cursorPos.x + 14
            const ty = cursorPos.y - 36
            return (
              <div
                className="pointer-events-none fixed z-50 px-2.5 py-1.5 rounded-md border border-slate-700 bg-slate-900/95 text-xs text-slate-200 shadow-lg max-w-[220px]"
                style={{ left: tx, top: ty }}
                aria-hidden="true"
              >
                <div className="font-medium truncate">{hovNode.label ?? hovNode.id}</div>
                {(callerCount > 0 || calleeCount > 0) && (
                  <div className="mt-0.5 text-slate-400 flex gap-2">
                    {callerCount > 0 && <span>{callerCount} caller{callerCount !== 1 ? 's' : ''}</span>}
                    {calleeCount > 0 && <span>{calleeCount} callee{calleeCount !== 1 ? 's' : ''}</span>}
                  </div>
                )}
              </div>
            )
          })()}

          {/* Node count + high-contrast toggle */}
          {!isLoading && !error && (
            <div className="absolute top-3 right-3 z-10 flex flex-col items-end gap-2">
              <div className="text-[10px] px-2 py-0.5 rounded border border-slate-700 bg-slate-800/80 text-slate-400 font-mono select-none">
                {nodes.length.toLocaleString()} nodes
                {totalNodeCount > nodes.length && ` / ${totalNodeCount.toLocaleString()} total`}
              </div>
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
