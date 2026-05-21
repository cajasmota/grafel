/**
 * useFocusNeighborhood — click-to-focus N-hop ego graph (#1392-refine item 5).
 *
 * When the user clicks a node we "focus" on its neighborhood: only that node
 * plus every node within N hops (BFS over the loaded edge list) stays visible;
 * everything else is hidden. This reads as a fresh, smaller graph of just the
 * related nodes, with the camera re-fit to it.
 *
 * State:
 *   focusNodeId — the clicked node id (null = no focus, full graph shown)
 *   hopDepth    — neighborhood radius in hops (default 2, clamped 1–3)
 *
 * The neighborhood itself is computed by the caller via `computeNeighborhood`
 * from the edges already present in the payload — NO backend call is made.
 *
 * hopDepth is persisted to localStorage so the user's preferred radius sticks.
 */
import { useState, useCallback } from 'react'
import type { GraphEdge } from '@/types/api'

const HOP_STORAGE_KEY = 'archigraph.graph.focusHopDepth'
export const MIN_HOP_DEPTH = 1
export const MAX_HOP_DEPTH = 3
export const DEFAULT_HOP_DEPTH = 2

function clampHop(v: number): number {
  if (!isFinite(v)) return DEFAULT_HOP_DEPTH
  return Math.max(MIN_HOP_DEPTH, Math.min(MAX_HOP_DEPTH, Math.round(v)))
}

function readStoredHop(): number {
  try {
    const raw = localStorage.getItem(HOP_STORAGE_KEY)
    if (raw) return clampHop(Number(raw))
  } catch {
    // private mode — fall through
  }
  return DEFAULT_HOP_DEPTH
}

export interface UseFocusNeighborhoodReturn {
  focusNodeId: string | null
  hopDepth: number
  setFocus: (id: string | null) => void
  clearFocus: () => void
  setHopDepth: (n: number) => void
  isFocused: boolean
}

export function useFocusNeighborhood(): UseFocusNeighborhoodReturn {
  const [focusNodeId, setFocusNodeId] = useState<string | null>(null)
  const [hopDepth, setHopDepthState] = useState<number>(readStoredHop)

  const setFocus = useCallback((id: string | null) => {
    setFocusNodeId(id)
  }, [])

  const clearFocus = useCallback(() => setFocusNodeId(null), [])

  const setHopDepth = useCallback((n: number) => {
    const clamped = clampHop(n)
    setHopDepthState(clamped)
    try {
      localStorage.setItem(HOP_STORAGE_KEY, String(clamped))
    } catch {
      // ignore
    }
  }, [])

  return {
    focusNodeId,
    hopDepth,
    setFocus,
    clearFocus,
    setHopDepth,
    isFocused: focusNodeId !== null,
  }
}

/**
 * Build an undirected adjacency map from the edge list. Memoise this in the
 * caller against `edges` so a focus toggle / hop change doesn't rebuild it.
 */
export function buildAdjacency(edges: GraphEdge[]): Map<string, string[]> {
  const adj = new Map<string, string[]>()
  const push = (a: string, b: string) => {
    const list = adj.get(a)
    if (list) list.push(b)
    else adj.set(a, [b])
  }
  for (const e of edges) {
    const s = String(e.source)
    const t = String(e.target)
    push(s, t)
    push(t, s)
  }
  return adj
}

/**
 * BFS the N-hop neighborhood of `startId` over an undirected adjacency map.
 * Returns the set of node ids within `hopDepth` hops, INCLUDING startId.
 * Treated as undirected so both callers and callees are pulled in.
 */
export function computeNeighborhood(
  adj: Map<string, string[]>,
  startId: string,
  hopDepth: number,
): Set<string> {
  const result = new Set<string>([startId])
  let frontier: string[] = [startId]
  const depth = Math.max(MIN_HOP_DEPTH, Math.min(MAX_HOP_DEPTH, hopDepth))
  for (let hop = 0; hop < depth; hop++) {
    const next: string[] = []
    for (const id of frontier) {
      const neighbors = adj.get(id)
      if (!neighbors) continue
      for (const n of neighbors) {
        if (!result.has(n)) {
          result.add(n)
          next.push(n)
        }
      }
    }
    if (next.length === 0) break
    frontier = next
  }
  return result
}
