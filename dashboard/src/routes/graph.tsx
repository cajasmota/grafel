import { useState, useCallback, useEffect, useRef, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useGraphData } from '@/hooks/graph/useGraphData'
import { useGraphSelection } from '@/hooks/graph/useGraphSelection'
import { useEdgeKindFilters } from '@/hooks/graph/useEdgeKindFilters'
import { useEntityInspector } from '@/hooks/graph/useEntityInspector'
import { useCommunityColors } from '@/hooks/graph/useCommunityColors'
import { useGraphSearch } from '@/hooks/graph/useGraphSearch'
import { useHoverLabel } from '@/hooks/graph/useHoverLabel'
import { useColorMode } from '@/hooks/graph/useColorMode'
import { useGraphHighlight } from '@/hooks/graph/useGraphHighlight'
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
import { MCPActivityOverlay } from '@/components/graph/MCPActivityOverlay'
import { IndexingProgressModal } from '@/components/indexing/IndexingProgressModal'
import { repoColor } from '@/lib/colors'
import { RefreshCw } from 'lucide-react'
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
  const { hoveredNodeId, setHoveredNode, zoomToNode, resetView, fitView, resetZoom, toggleSimulation, graphRef } = useGraphCameraStore()
  const simulationRunning = useSimulationRunning()

  // ── Color mode (#1153 — persisted to localStorage) ────────────────────────
  const { colorMode, setColorMode } = useColorMode()

  // ── View state ─────────────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearchResults, setShowSearchResults] = useState(false)
  const [highContrast, setHighContrast] = useState(false)
  const [reindexOpen, setReindexOpen] = useState(false)
  const [crossRepoOnly, setCrossRepoOnly] = useState(false)
  const [hoveredCommunityId, setHoveredCommunityId] = useState<number | null>(null)

  // ── Hover tooltip state (#1060) ────────────────────────────────────────────
  // Track cursor position in screen coordinates for tooltip placement
  const [cursorPos, setCursorPos] = useState<{ x: number; y: number } | null>(null)
  const searchContainerRef = useRef<HTMLDivElement>(null)

  // ── External node visibility state (#1067) ─────────────────────────────────
  // Hidden by default — External stdlib/builtin placeholders clutter the graph.
  const [showExternal, setShowExternal] = useState(false)

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

  // #1069: compute effectiveActiveRepos for passing to GraphCanvas (client-side filter).
  // null = all repos visible (no filter chip active or all repos selected).
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
      // #1069: activeRepos no longer passed to the hook — repo filter is client-side only.
      // The hook ignores this param but it's kept in the signature for compat.
      effectiveActiveRepos,
      showExternal,
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

  // #1069: close the inspector when the selected node's repo is filtered out.
  // This prevents the inspector showing data for a node that is visually hidden.
  useEffect(() => {
    if (!selectedNodeId || !effectiveActiveRepos) return
    const selectedNode = nodes.find((n) => n.id === selectedNodeId)
    if (selectedNode && !effectiveActiveRepos.has(selectedNode.repo)) {
      clearSelection()
    }
  }, [effectiveActiveRepos, selectedNodeId, nodes, clearSelection])

  // ── Inspector ──────────────────────────────────────────────────────────────
  const { data: inspectorData, isLoading: inspectorLoading } = useEntityInspector(
    group ?? '',
    selectedNodeId,
  )

  // ── Hover-to-label (Tier 2) ────────────────────────────────────────────────
  // When a hovered node was not in the initial top-200 label fetch its label
  // falls back to the raw id.  useHoverLabel detects that sentinel and fires a
  // one-shot /labels?ids= fetch, seeding the result back into react-query cache
  // so the node label updates on the next render cycle.
  const hoveredNode = hoveredNodeId ? nodes.find((n) => n.id === hoveredNodeId) : undefined
  useHoverLabel(group ?? '', hoveredNodeId, hoveredNode?.label)

  // ── Search ─────────────────────────────────────────────────────────────────
  const { results: searchResults, isSearching } = useGraphSearch(searchQuery, nodes)
  useEffect(() => {
    setShowSearchResults(searchQuery.length > 0)
  }, [searchQuery])

  // ── Handlers ───────────────────────────────────────────────────────────────
  const handleNodeClick = useCallback((node: GraphNode) => {
    selectNode(node.id)
  }, [selectNode])

  /** Clears node selection: removes URL param + signals Cosmograph to unhighlight. */
  const handleDeselect = useCallback(() => {
    clearSelection()
    graphRef?.unselectPoints?.()
  }, [clearSelection, graphRef])

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
          handleDeselect()
        }
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [handleDeselect, showSearchResults])

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

  const handleOpenInPaths = useCallback((entityId: string) => {
    navigate(`/${group}/paths?highlight=${encodeURIComponent(entityId)}`)
  }, [group, navigate])

  const handleOpenInTopology = useCallback((entityId: string) => {
    navigate(`/${group}/topology?highlight=${encodeURIComponent(entityId)}`)
  }, [group, navigate])

  // ── Inspector pin state ────────────────────────────────────────────────────
  const [inspectorPinned, setInspectorPinned] = useState(false)
  const handleTogglePin = useCallback(() => setInspectorPinned((p) => !p), [])

  // When pinned, deselecting a node does NOT close the panel (the pin holds it).
  const handleDeselectWithPin = useCallback(() => {
    if (!inspectorPinned) handleDeselect()
  }, [inspectorPinned, handleDeselect])

  // ── Highlight subgraph (uses useGraphHighlight imperative API) ─────────────
  const {
    highlightedNodeIds,
    highlightedEdgeIds,
    isActive: mcpHighlightActive,
    agentId: mcpAgentId,
    enabled: mcpOverlayEnabled,
    setEnabled: setMcpOverlayEnabled,
    sseConnected: mcpSseConnected,
    eventLog: mcpEventLog,
    totalCount: mcpTotalCount,
    replayHighlight: mcpReplayHighlight,
    highlight: imperativeHighlight,
  } = useGraphHighlight()

  const handleHighlightSubgraph = useCallback((nodeIds: string[]) => {
    imperativeHighlight(nodeIds, [], 8000)
  }, [imperativeHighlight])

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
      {/* Rebuild index floating button */}
      <button
        type="button"
        onClick={() => setReindexOpen(true)}
        className={[
          'fixed bottom-4 left-[200px] z-30',
          'flex items-center gap-1 text-[10px] px-2 py-1 rounded border transition-colors shadow-md',
          'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400',
          'border-slate-300 dark:border-slate-700',
          'hover:text-sky-400 hover:border-sky-600',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
        ].join(' ')}
        aria-label="Rebuild index"
        data-testid="graph-reindex-btn"
        title="Rebuild index for this group"
      >
        <RefreshCw className="w-2.5 h-2.5" />
        Rebuild
      </button>

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
          crossRepoOnly={crossRepoOnly}
          onCrossRepoOnlyChange={setCrossRepoOnly}
          showExternal={showExternal}
          onToggleExternal={() => setShowExternal((v) => !v)}
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
        {/* Left: color mode + repo filter + community legend */}
        <aside
          className="hidden lg:flex flex-col w-48 min-w-[160px] border-r border-slate-200 dark:border-slate-800 bg-white/80 dark:bg-slate-950/80 p-2 gap-2 overflow-y-auto"
          aria-label="Graph filters sidebar"
        >
          {/* Color mode toggle (#1153) */}
          <div>
            <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold px-2 pb-1">
              Color by
            </p>
            <div className="flex flex-col gap-0.5" role="radiogroup" aria-label="Graph color mode">
              {(
                [
                  { value: 'repo',      label: 'Repo',      title: 'Color nodes by repository (default)' },
                  { value: 'community', label: 'Community', title: 'Color nodes by community cluster' },
                  { value: 'degree',    label: 'Degree',    title: 'Silk Road gradient — purple→pink→yellow by connection count' },
                ] as const
              ).map(({ value, label, title }) => (
                <button
                  key={value}
                  type="button"
                  role="radio"
                  aria-checked={colorMode === value}
                  onClick={() => setColorMode(value)}
                  title={title}
                  className={[
                    'flex items-center gap-2 px-2 py-1 rounded text-left text-xs w-full transition-colors',
                    'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                    colorMode === value
                      ? 'bg-sky-900/40 text-sky-300 dark:text-sky-300'
                      : 'text-slate-600 dark:text-slate-400 hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
                  ].join(' ')}
                >
                  {/* Active indicator dot */}
                  <span
                    className={[
                      'w-1.5 h-1.5 rounded-full shrink-0 transition-all',
                      colorMode === value ? 'bg-sky-400' : 'bg-slate-600',
                    ].join(' ')}
                    aria-hidden
                  />
                  {label}
                </button>
              ))}
            </div>
          </div>

          <div className="border-t border-slate-200 dark:border-slate-700" />

          {/* MCP activity overlay toggle (#1157, Phase 2) */}
          <div>
            <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold px-2 pb-1">
              Overlays
            </p>
            <button
              type="button"
              role="switch"
              aria-checked={mcpOverlayEnabled}
              onClick={() => setMcpOverlayEnabled(!mcpOverlayEnabled)}
              title={mcpOverlayEnabled ? 'Disable MCP activity overlay' : 'Enable MCP activity overlay'}
              className={[
                'flex items-center gap-2 px-2 py-1 rounded text-left text-xs w-full transition-colors',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                mcpOverlayEnabled
                  ? 'bg-sky-900/40 text-sky-300 dark:text-sky-300'
                  : 'text-slate-600 dark:text-slate-400 hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
              ].join(' ')}
            >
              <span
                className={[
                  'w-1.5 h-1.5 rounded-full shrink-0 transition-all',
                  mcpOverlayEnabled
                    ? mcpHighlightActive ? 'bg-sky-300 animate-pulse' : 'bg-sky-400'
                    : 'bg-slate-600',
                ].join(' ')}
                aria-hidden
              />
              MCP activity
            </button>
          </div>

          <div className="border-t border-slate-200 dark:border-slate-700" />

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
            {communities.length === 0 && !isLoading ? (
              /* Empty community filter hint (#1153 polish) */
              <p className="px-2 py-2 text-[10px] text-slate-500 dark:text-slate-600 leading-relaxed">
                No communities visible.{' '}
                {selectedCommunityId !== null ? (
                  <button
                    type="button"
                    onClick={handleClearCommunity}
                    className="text-sky-500 hover:text-sky-400 underline underline-offset-1 focus-visible:outline-none"
                  >
                    Try expanding the filter
                  </button>
                ) : (
                  'Try expanding the filter or loading more data.'
                )}
              </p>
            ) : (
              <CommunityLegend
                communities={communities}
                colorMap={colorMap}
                highlightId={hoveredCommunityId}
                onHover={setHoveredCommunityId}
                onSelect={handleCommunityClick}
                selectedId={selectedCommunityId}
              />
            )}
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
              onEmptyClick={handleDeselect}
              highContrast={highContrast}
              isDark={isDark}
              crossRepoOnly={crossRepoOnly}
              simulationRunning={simulationRunning}
              onSimulationRunningChange={toggleSimulation}
              className="w-full h-full"
              activeRepos={effectiveActiveRepos}
              colorMode={colorMode}
              forceVisibleIds={highlightedNodeIds}
              highlightedEdgeIds={highlightedEdgeIds}
            />
          )}

          {/* #1157 Phase 2: Jarvis MCP activity overlay — pulse badge + log panel */}
          <MCPActivityOverlay
            enabled={mcpOverlayEnabled}
            connected={mcpSseConnected}
            isActive={mcpHighlightActive}
            agentId={mcpAgentId}
            totalCount={mcpTotalCount}
            eventLog={mcpEventLog}
            onReplay={mcpReplayHighlight}
            isDark={isDark}
          />

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

          {/* Indexing progress modal — triggered by Rebuild button */}
          {group && (
            <IndexingProgressModal
              isOpen={reindexOpen}
              groupSlug={group}
              onClose={() => setReindexOpen(false)}
            />
          )}
        </div>

        {/* Right: entity inspector */}
        {showInspector && (
          <EntityInspector
            data={inspectorData}
            isLoading={inspectorLoading}
            onClose={handleDeselectWithPin}
            onSelectEntity={(id) => {
              selectNode(id)
              zoomToNode(id)
            }}
            group={group}
            onOpenInFlows={handleOpenInFlows}
            onOpenInPaths={handleOpenInPaths}
            onOpenInTopology={handleOpenInTopology}
            onHighlightSubgraph={handleHighlightSubgraph}
            pinned={inspectorPinned}
            onTogglePin={handleTogglePin}
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
