/**
 * useGraphFilter — multi-criteria graph filter state (#1367).
 *
 * Criteria:
 *   - kinds:         Set<EntityKind>  (multi-select; empty = all)
 *   - communityIds:  Set<number>      (multi-select; empty = all)
 *   - minDegree:     number           (0 = no threshold)
 *   - fileGlob:      string           (glob against source_file; '' = no filter)
 *   - hasProperty:   string           ('' = no filter; 'description' | 'domain' | etc.)
 *
 * State is persisted to localStorage under the key 'archigraph:graph-filter'.
 * Exports `filterNodes()` — a pure function that applies all criteria to a
 * GraphNode[] and returns the array of matching indices (for Cosmograph selectPoints).
 *
 * Design notes
 * ────────────
 * • `activeFilter` is truthy only when at least one criterion is non-default.
 *   Callers can short-circuit to null (= show all) when it's false.
 * • URL-shareable filter hash is exposed via `filterHash` — consumers can
 *   append it to the page URL with `history.replaceState` if desired.
 * • `invertFilter` flag is supported: when true, hidden nodes become visible
 *   and vice versa (applies AFTER the normal criteria pass).
 * • `filterLogic` = 'and' | 'or' switches between intersection and union.
 *   Default is 'and' (all criteria must match).
 */

import { useState, useCallback, useMemo } from 'react'
import type { GraphNode, EntityKind } from '@/types/api'

// ────────────────────────────────────────────────────────────────────────────
// Types
// ────────────────────────────────────────────────────────────────────────────

export interface GraphFilterState {
  kinds:        Set<EntityKind>
  communityIds: Set<number>
  minDegree:    number
  fileGlob:     string
  hasProperty:  string
  invertFilter: boolean
  filterLogic:  'and' | 'or'
}

const DEFAULT_FILTER: GraphFilterState = {
  kinds:        new Set(),
  communityIds: new Set(),
  minDegree:    0,
  fileGlob:     '',
  hasProperty:  '',
  invertFilter: false,
  filterLogic:  'and',
}

// ────────────────────────────────────────────────────────────────────────────
// localStorage serialise / deserialise
// ────────────────────────────────────────────────────────────────────────────

const LS_KEY = 'archigraph:graph-filter'

function serialise(f: GraphFilterState): string {
  return JSON.stringify({
    kinds:        [...f.kinds],
    communityIds: [...f.communityIds],
    minDegree:    f.minDegree,
    fileGlob:     f.fileGlob,
    hasProperty:  f.hasProperty,
    invertFilter: f.invertFilter,
    filterLogic:  f.filterLogic,
  })
}

function deserialise(raw: string): GraphFilterState {
  try {
    const obj = JSON.parse(raw)
    return {
      kinds:        new Set<EntityKind>(obj.kinds ?? []),
      communityIds: new Set<number>((obj.communityIds ?? []).map(Number)),
      minDegree:    typeof obj.minDegree === 'number' ? obj.minDegree : 0,
      fileGlob:     typeof obj.fileGlob   === 'string' ? obj.fileGlob  : '',
      hasProperty:  typeof obj.hasProperty === 'string' ? obj.hasProperty : '',
      invertFilter: obj.invertFilter === true,
      filterLogic:  obj.filterLogic === 'or' ? 'or' : 'and',
    }
  } catch {
    return { ...DEFAULT_FILTER }
  }
}

function loadFromStorage(): GraphFilterState {
  try {
    const raw = typeof localStorage !== 'undefined' ? localStorage.getItem(LS_KEY) : null
    return raw ? deserialise(raw) : { ...DEFAULT_FILTER }
  } catch {
    return { ...DEFAULT_FILTER }
  }
}

// ────────────────────────────────────────────────────────────────────────────
// Glob matcher (path matching only — no full picomatch)
// ────────────────────────────────────────────────────────────────────────────

/**
 * Simple glob match: supports `*` (any chars within segment) and `**` (any path).
 * Case-insensitive. Returns true when `glob` is empty.
 */
export function matchGlob(path: string | undefined, glob: string): boolean {
  if (!glob) return true
  if (!path) return false
  // Convert glob to regex: ** → .*, * → [^/]*
  const escaped = glob
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')  // escape regex specials (not * or ?)
    .replace(/\*\*/g, '\x00')               // placeholder for **
    .replace(/\*/g,   '[^/]*')              // * → match within segment
    .replace(/\x00/g, '.*')                 // ** → match across segments
  try {
    return new RegExp('^' + escaped + '$', 'i').test(path)
  } catch {
    return path.toLowerCase().includes(glob.toLowerCase())
  }
}

// ────────────────────────────────────────────────────────────────────────────
// filterNodes — pure function
// ────────────────────────────────────────────────────────────────────────────

/**
 * Returns the array of indices in `nodes` that pass all active criteria.
 * Returns null when no criteria are active (callers should pass null to selectPoints).
 */
export function filterNodes(
  nodes: GraphNode[],
  filter: GraphFilterState,
): number[] | null {
  const isDefault =
    filter.kinds.size === 0 &&
    filter.communityIds.size === 0 &&
    filter.minDegree === 0 &&
    !filter.fileGlob &&
    !filter.hasProperty

  if (isDefault && !filter.invertFilter) return null

  const isAnd = filter.filterLogic === 'and'

  const result: number[] = []
  for (let i = 0; i < nodes.length; i++) {
    const n = nodes[i]
    const checks: boolean[] = []

    if (filter.kinds.size > 0) {
      checks.push(filter.kinds.has(n.kind))
    }
    if (filter.communityIds.size > 0) {
      checks.push(n.community_id !== undefined && filter.communityIds.has(n.community_id))
    }
    if (filter.minDegree > 0) {
      checks.push((n.degree ?? 0) >= filter.minDegree)
    }
    if (filter.fileGlob) {
      checks.push(matchGlob(n.source_file, filter.fileGlob))
    }
    if (filter.hasProperty) {
      checks.push(
        !!n.properties && filter.hasProperty in n.properties && n.properties[filter.hasProperty] !== null && n.properties[filter.hasProperty] !== '',
      )
    }

    if (checks.length === 0) {
      // No active criteria — node passes by default
      result.push(i)
      continue
    }

    const passes = isAnd ? checks.every(Boolean) : checks.some(Boolean)
    const effective = filter.invertFilter ? !passes : passes
    if (effective) result.push(i)
  }

  return result
}

// ────────────────────────────────────────────────────────────────────────────
// URL hash serialiser (for #filter= sharing)
// ────────────────────────────────────────────────────────────────────────────

export function filterToHash(f: GraphFilterState): string {
  const isDefault =
    f.kinds.size === 0 &&
    f.communityIds.size === 0 &&
    f.minDegree === 0 &&
    !f.fileGlob &&
    !f.hasProperty &&
    !f.invertFilter &&
    f.filterLogic === 'and'
  if (isDefault) return ''
  try {
    return '#filter=' + encodeURIComponent(serialise(f))
  } catch {
    return ''
  }
}

// ────────────────────────────────────────────────────────────────────────────
// Hook
// ────────────────────────────────────────────────────────────────────────────

export interface UseGraphFilterReturn {
  filter:           GraphFilterState
  activeFilter:     boolean
  filterHash:       string
  setKinds:         (kinds: Set<EntityKind>) => void
  setCommunityIds:  (ids: Set<number>) => void
  setMinDegree:     (v: number) => void
  setFileGlob:      (v: string) => void
  setHasProperty:   (v: string) => void
  setInvert:        (v: boolean) => void
  setLogic:         (v: 'and' | 'or') => void
  clearAll:         () => void
}

export function useGraphFilter(): UseGraphFilterReturn {
  const [filter, setFilter] = useState<GraphFilterState>(() => loadFromStorage())

  const persist = useCallback((next: GraphFilterState) => {
    setFilter(next)
    try {
      localStorage.setItem(LS_KEY, serialise(next))
    } catch { /* quota exceeded — ignore */ }
  }, [])

  const setKinds = useCallback((kinds: Set<EntityKind>) => {
    setFilter((f) => { const next = { ...f, kinds }; persist(next); return next })
  }, [persist])

  const setCommunityIds = useCallback((ids: Set<number>) => {
    setFilter((f) => { const next = { ...f, communityIds: ids }; persist(next); return next })
  }, [persist])

  const setMinDegree = useCallback((v: number) => {
    setFilter((f) => { const next = { ...f, minDegree: v }; persist(next); return next })
  }, [persist])

  const setFileGlob = useCallback((v: string) => {
    setFilter((f) => { const next = { ...f, fileGlob: v }; persist(next); return next })
  }, [persist])

  const setHasProperty = useCallback((v: string) => {
    setFilter((f) => { const next = { ...f, hasProperty: v }; persist(next); return next })
  }, [persist])

  const setInvert = useCallback((v: boolean) => {
    setFilter((f) => { const next = { ...f, invertFilter: v }; persist(next); return next })
  }, [persist])

  const setLogic = useCallback((v: 'and' | 'or') => {
    setFilter((f) => { const next = { ...f, filterLogic: v }; persist(next); return next })
  }, [persist])

  const clearAll = useCallback(() => {
    const next = { ...DEFAULT_FILTER }
    persist(next)
  }, [persist])

  const activeFilter = useMemo(() => (
    filter.kinds.size > 0 ||
    filter.communityIds.size > 0 ||
    filter.minDegree > 0 ||
    !!filter.fileGlob ||
    !!filter.hasProperty
  ), [filter])

  const filterHash = useMemo(() => filterToHash(filter), [filter])

  return {
    filter,
    activeFilter,
    filterHash,
    setKinds,
    setCommunityIds,
    setMinDegree,
    setFileGlob,
    setHasProperty,
    setInvert,
    setLogic,
    clearAll,
  }
}
