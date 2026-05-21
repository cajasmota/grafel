/**
 * useGraphKeyboardNav — keyboard-driven node navigation on the graph canvas.
 *
 * Closes #1368.
 *
 * Bindings (only fire when no text input is active):
 *   ArrowUp    → jump to highest-pagerank OUTBOUND neighbor of selected node
 *   ArrowDown  → jump to highest-pagerank INBOUND neighbor
 *   ArrowLeft  → jump to alphabetically-previous neighbor (label comparison)
 *   ArrowRight → jump to alphabetically-next neighbor
 *   Tab        → cycle through all neighbors of selected node (forward)
 *   Shift+Tab  → cycle through all neighbors (backward)
 *   Enter      → open the EntityInspector for the selected node
 *   n / N      → select next search-match node
 *   p / P      → select previous search-match node
 *   f / F      → fit the viewport (delegates to graphRef.fitView)
 *   0          → reset zoom (delegates to graphRef.setZoomLevel(1))
 *   Cmd+[      → navigate backward in history stack
 *   Cmd+]      → navigate forward in history stack
 *
 * The hook returns:
 *   - `navHistory`  — read-only array of visited node ids (newest last)
 *   - `historyIdx`  — current position in navHistory
 *
 * Architecture notes:
 *   - All logic is purely in-memory — edges are provided by the caller from
 *     useGraphData so we never need an extra fetch.
 *   - `zoomToNode` from graphCameraStore is called on every selection change
 *     so Cosmograph pans + zooms to the new selection automatically.
 *   - Tab cycling state is local to this hook and resets when selectedNodeId
 *     changes by any other means (click, search-select, URL change).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'

// ─── helpers ────────────────────────────────────────────────────────────────

function isInputActive(): boolean {
  const el = document.activeElement
  return (
    el instanceof HTMLInputElement ||
    el instanceof HTMLTextAreaElement ||
    (el instanceof HTMLElement && el.isContentEditable)
  )
}

/** Returns true for Mac (⌘) modifier, false for Ctrl on others. */
const isMac =
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform)

function modPressed(e: KeyboardEvent): boolean {
  return isMac ? e.metaKey : e.ctrlKey
}

// ─── types ───────────────────────────────────────────────────────────────────

export interface UseGraphKeyboardNavOptions {
  /** Full node list from useGraphData */
  nodes: GraphNode[]
  /** Full edge list from useGraphData */
  edges: GraphEdge[]
  /** Currently selected node id (from useGraphSelection) */
  selectedNodeId: string | null
  /** Selects a node and updates URL param */
  selectNode: (id: string | null) => void
  /** Clears the node selection */
  clearSelection: () => void
  /** Ordered array of search-result node ids (from useGraphSearch) */
  searchResultIds: string[]
  /** Whether the canvas is mounted and data is available */
  enabled: boolean
}

export interface UseGraphKeyboardNavReturn {
  /** Recently-visited node id trail (newest last, capped at 50) */
  navHistory: readonly string[]
  /** Current index in navHistory (-1 = outside stack / no history) */
  historyIdx: number
}

// ─── hook ────────────────────────────────────────────────────────────────────

const HISTORY_CAP = 50

export function useGraphKeyboardNav({
  nodes,
  edges,
  selectedNodeId,
  selectNode,
  clearSelection,
  searchResultIds,
  enabled,
}: UseGraphKeyboardNavOptions): UseGraphKeyboardNavReturn {
  // ── Camera store ──────────────────────────────────────────────────────────
  const { zoomToNode, fitView, graphRef } = useGraphCameraStore()

  // ── Navigation history stack ──────────────────────────────────────────────
  // `historyRef` is the source-of-truth; `history`/`historyIdx` are React state
  // for consumers that need to re-render on changes.
  const historyRef  = useRef<string[]>([])
  const historyIdxRef = useRef<number>(-1)

  const [history, setHistory]       = useState<readonly string[]>([])
  const [historyIdx, setHistoryIdx] = useState<number>(-1)

  // Keep stable references to nodes / edges / selection so event listeners
  // don't capture stale closures.
  const nodesRef         = useRef<GraphNode[]>(nodes)
  const edgesRef         = useRef<GraphEdge[]>(edges)
  const selectedIdRef    = useRef<string | null>(selectedNodeId)
  const searchIdsRef     = useRef<string[]>(searchResultIds)
  const selectNodeRef    = useRef(selectNode)
  const clearSelRef      = useRef(clearSelection)
  const zoomToNodeRef    = useRef(zoomToNode)
  const fitViewRef       = useRef(fitView)
  const graphRefRef      = useRef(graphRef)

  nodesRef.current      = nodes
  edgesRef.current      = edges
  selectedIdRef.current = selectedNodeId
  searchIdsRef.current  = searchResultIds
  selectNodeRef.current = selectNode
  clearSelRef.current   = clearSelection
  zoomToNodeRef.current = zoomToNode
  fitViewRef.current    = fitView
  graphRefRef.current   = graphRef

  // ── Tab-cycle state ───────────────────────────────────────────────────────
  // Resets when selectedNodeId changes by any external means.
  const tabCycleRef = useRef<{ nodeId: string; idx: number } | null>(null)

  // ── Push to history ───────────────────────────────────────────────────────
  const pushHistory = useCallback((id: string) => {
    const arr  = historyRef.current
    const cur  = historyIdxRef.current

    // If we've gone back and are now branching, truncate forward items
    const base = cur >= 0 ? arr.slice(0, cur + 1) : arr

    // Deduplicate: don't push same node twice in a row
    if (base[base.length - 1] === id) {
      historyRef.current    = base
      historyIdxRef.current = base.length - 1
      setHistory(base)
      setHistoryIdx(base.length - 1)
      return
    }

    const next = [...base, id].slice(-HISTORY_CAP)
    historyRef.current    = next
    historyIdxRef.current = next.length - 1
    setHistory(next)
    setHistoryIdx(next.length - 1)
  }, [])

  // ── Select + zoom + push history ─────────────────────────────────────────
  const selectAndZoom = useCallback((id: string) => {
    selectNodeRef.current(id)
    zoomToNodeRef.current(id)
    pushHistory(id)
    // Reset tab-cycle when actively navigating
    tabCycleRef.current = null
  }, [pushHistory])

  // ── Sync history when selectedNodeId changes externally ───────────────────
  // (e.g. via click or search-select)
  useEffect(() => {
    if (!selectedNodeId) return
    pushHistory(selectedNodeId)
  // Only run when the id itself changes
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedNodeId])

  // ── Adjacency helpers ─────────────────────────────────────────────────────

  /**
   * Returns the set of outbound neighbor node ids for `nodeId`.
   * Handles both string and number id types by coercing to string.
   */
  function outNeighborIds(nodeId: string): string[] {
    const result: string[] = []
    for (const e of edgesRef.current) {
      if (String(e.source) === nodeId) result.push(String(e.target))
    }
    return result
  }

  /**
   * Returns the set of inbound neighbor node ids for `nodeId`.
   */
  function inNeighborIds(nodeId: string): string[] {
    const result: string[] = []
    for (const e of edgesRef.current) {
      if (String(e.target) === nodeId) result.push(String(e.source))
    }
    return result
  }

  /**
   * Returns all unique neighbor ids (both in and out) for `nodeId`.
   */
  function allNeighborIds(nodeId: string): string[] {
    const seen = new Set<string>()
    for (const id of [...outNeighborIds(nodeId), ...inNeighborIds(nodeId)]) {
      seen.add(id)
    }
    return [...seen]
  }

  /**
   * Resolves a node id to a GraphNode, or undefined.
   */
  function nodeById(id: string): GraphNode | undefined {
    return nodesRef.current.find((n) => String(n.id) === id)
  }

  // ── Key handler ───────────────────────────────────────────────────────────

  useEffect(() => {
    if (!enabled) return

    function handle(e: KeyboardEvent) {
      // Never fire when user is typing in an input / textarea / contenteditable
      if (isInputActive()) return

      const selId = selectedIdRef.current

      // ── Cmd+[ / Cmd+] — history back / forward ─────────────────────────
      if (modPressed(e) && e.key === '[') {
        e.preventDefault()
        const arr = historyRef.current
        const idx = historyIdxRef.current
        if (idx > 0) {
          const newIdx = idx - 1
          historyIdxRef.current = newIdx
          setHistoryIdx(newIdx)
          selectNodeRef.current(arr[newIdx])
          zoomToNodeRef.current(arr[newIdx])
          tabCycleRef.current = null
        }
        return
      }

      if (modPressed(e) && e.key === ']') {
        e.preventDefault()
        const arr = historyRef.current
        const idx = historyIdxRef.current
        if (idx < arr.length - 1) {
          const newIdx = idx + 1
          historyIdxRef.current = newIdx
          setHistoryIdx(newIdx)
          selectNodeRef.current(arr[newIdx])
          zoomToNodeRef.current(arr[newIdx])
          tabCycleRef.current = null
        }
        return
      }

      // ── F — fit view ──────────────────────────────────────────────────────
      if (e.key === 'f' || e.key === 'F') {
        if (modPressed(e)) return // Cmd+F = browser find, don't steal
        e.preventDefault()
        fitViewRef.current()
        return
      }

      // ── 0 — reset zoom ────────────────────────────────────────────────────
      if (e.key === '0') {
        e.preventDefault()
        const ref = graphRefRef.current
        if (ref?.setZoomLevel) ref.setZoomLevel(1, 300)
        return
      }

      // ── / — focus search input ────────────────────────────────────────────
      if (e.key === '/') {
        e.preventDefault()
        document.getElementById('graph-search')?.focus()
        return
      }

      // ── n / N — next search match ─────────────────────────────────────────
      if (e.key === 'n' || e.key === 'N') {
        e.preventDefault()
        const ids = searchIdsRef.current
        if (ids.length === 0) return
        const cur = ids.indexOf(selId ?? '')
        const next = ids[(cur + 1) % ids.length]
        if (next) selectAndZoom(next)
        return
      }

      // ── p / P — previous search match ────────────────────────────────────
      if (e.key === 'p' || e.key === 'P') {
        e.preventDefault()
        const ids = searchIdsRef.current
        if (ids.length === 0) return
        const cur = ids.indexOf(selId ?? '')
        const prev = ids[(cur - 1 + ids.length) % ids.length]
        if (prev) selectAndZoom(prev)
        return
      }

      // ── Everything below requires a selected node ─────────────────────────
      if (!selId) return

      // ── Enter — open detail panel (already handled by selection, but ensure
      //            inspector shows if hidden; do nothing extra — selection state
      //            drives the inspector in graph.tsx) ─────────────────────────
      if (e.key === 'Enter') {
        // Selection is already in URL. Trigger a re-select to ensure inspector
        // renders in case it was closed without deselecting.
        e.preventDefault()
        selectNodeRef.current(selId)
        return
      }

      // ── Tab / Shift+Tab — cycle neighbors ────────────────────────────────
      if (e.key === 'Tab') {
        e.preventDefault()
        const neighbors = allNeighborIds(selId).sort((a, b) => {
          const na = nodeById(a)?.label ?? a
          const nb = nodeById(b)?.label ?? b
          return na.localeCompare(nb)
        })
        if (neighbors.length === 0) return

        const cycle = tabCycleRef.current
        let newIdx: number

        if (!cycle || cycle.nodeId !== selId) {
          // First Tab press for this node
          newIdx = e.shiftKey ? neighbors.length - 1 : 0
        } else {
          const delta = e.shiftKey ? -1 : 1
          newIdx = (cycle.idx + delta + neighbors.length) % neighbors.length
        }

        tabCycleRef.current = { nodeId: selId, idx: newIdx }
        const targetId = neighbors[newIdx]
        if (targetId) selectAndZoom(targetId)
        return
      }

      // ── Arrow keys — navigate by edge direction + sorted order ────────────
      if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return
      e.preventDefault()

      const outIds = outNeighborIds(selId)
      const inIds  = inNeighborIds(selId)
      const allIds = allNeighborIds(selId)

      if (allIds.length === 0) return

      let targetId: string | undefined

      if (e.key === 'ArrowUp') {
        // Up = highest-pagerank outbound neighbor
        const candidates = outIds.length > 0 ? outIds : allIds
        targetId = candidates
          .map((id) => ({ id, pr: nodeById(id)?.pagerank ?? 0 }))
          .sort((a, b) => b.pr - a.pr)[0]?.id
      } else if (e.key === 'ArrowDown') {
        // Down = highest-pagerank inbound neighbor
        const candidates = inIds.length > 0 ? inIds : allIds
        targetId = candidates
          .map((id) => ({ id, pr: nodeById(id)?.pagerank ?? 0 }))
          .sort((a, b) => b.pr - a.pr)[0]?.id
      } else if (e.key === 'ArrowLeft') {
        // Left = alphabetically previous neighbor
        const sorted = allIds
          .map((id) => ({ id, label: nodeById(id)?.label ?? id }))
          .sort((a, b) => a.label.localeCompare(b.label))
        const curLabel = nodeById(selId)?.label ?? selId
        const idx = sorted.findIndex((x) => x.label >= curLabel)
        const prevIdx = idx <= 0 ? sorted.length - 1 : idx - 1
        targetId = sorted[prevIdx]?.id
      } else if (e.key === 'ArrowRight') {
        // Right = alphabetically next neighbor
        const sorted = allIds
          .map((id) => ({ id, label: nodeById(id)?.label ?? id }))
          .sort((a, b) => a.label.localeCompare(b.label))
        const curLabel = nodeById(selId)?.label ?? selId
        const idx = sorted.findIndex((x) => x.label > curLabel)
        targetId = idx === -1 ? sorted[0]?.id : sorted[idx]?.id
      }

      if (targetId) selectAndZoom(targetId)
    }

    window.addEventListener('keydown', handle)
    return () => window.removeEventListener('keydown', handle)
  }, [enabled, selectAndZoom])

  return { navHistory: history, historyIdx }
}
