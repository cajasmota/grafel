/**
 * useNodeSizingConfig — persisted tunable node sizing config.
 *
 * Issue #1360: expose live tuning controls so the user can dial in graph node
 * sizes without guessing.
 *
 * State:
 *   baseSize    — numeric (px), default 10
 *   multipliers — 6-element array, one per degree-percentile tier
 *
 * Tiers (degree percentile bands):
 *   0 → lowest 50%        → 1.0×
 *   1 → 50–75%            → 1.5×
 *   2 → 75–90%            → 2.0×
 *   3 → 90–97%            → 3.0×
 *   4 → 97–99.5%          → 5.0×
 *   5 → top 0.5%          → 10.0×
 *
 * All values are persisted to localStorage under key `archigraph:graph:sizing`.
 */
import { useState, useCallback } from 'react'

export const TIER_COUNT = 6

export const DEFAULT_BASE_SIZE = 10

export const DEFAULT_MULTIPLIERS: readonly number[] = [1.0, 1.5, 2.0, 3.0, 5.0, 10.0]

/**
 * Percentile upper-bounds per tier (inclusive, 0-100).
 * Tier 0: [0, 50], Tier 1: (50, 75], ..., Tier 5: (99.5, 100]
 */
export const TIER_UPPER_PERCENTILES: readonly number[] = [50, 75, 90, 97, 99.5, 100]

export interface NodeSizingConfig {
  baseSize: number
  multipliers: number[]
}

export interface UseNodeSizingConfigReturn {
  config: NodeSizingConfig
  setBaseSize: (v: number) => void
  setMultiplier: (tierIndex: number, v: number) => void
  resetToDefaults: () => void
}

const STORAGE_KEY = 'archigraph:graph:sizing'

function loadFromStorage(): NodeSizingConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<NodeSizingConfig>
    const baseSize = typeof parsed.baseSize === 'number' && isFinite(parsed.baseSize)
      ? Math.max(2, Math.min(50, parsed.baseSize))
      : DEFAULT_BASE_SIZE
    const multipliers = Array.isArray(parsed.multipliers) && parsed.multipliers.length === TIER_COUNT
      ? (parsed.multipliers as number[]).map((m) =>
          typeof m === 'number' && isFinite(m) ? Math.max(0.1, Math.min(100, m)) : 1.0,
        )
      : [...DEFAULT_MULTIPLIERS]
    return { baseSize, multipliers }
  } catch {
    return null
  }
}

function saveToStorage(cfg: NodeSizingConfig): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
  } catch {
    // localStorage may be unavailable in some environments — fail silently
  }
}

function defaultConfig(): NodeSizingConfig {
  return { baseSize: DEFAULT_BASE_SIZE, multipliers: [...DEFAULT_MULTIPLIERS] }
}

export function useNodeSizingConfig(): UseNodeSizingConfigReturn {
  const [config, setConfig] = useState<NodeSizingConfig>(() => loadFromStorage() ?? defaultConfig())

  const update = useCallback((next: NodeSizingConfig) => {
    setConfig(next)
    saveToStorage(next)
  }, [])

  const setBaseSize = useCallback((v: number) => {
    const clamped = Math.max(2, Math.min(50, isFinite(v) ? v : DEFAULT_BASE_SIZE))
    setConfig((prev) => {
      const next = { ...prev, baseSize: clamped }
      saveToStorage(next)
      return next
    })
  }, [])

  const setMultiplier = useCallback((tierIndex: number, v: number) => {
    const clamped = Math.max(0.1, Math.min(100, isFinite(v) ? v : 1.0))
    setConfig((prev) => {
      const multipliers = [...prev.multipliers]
      multipliers[tierIndex] = clamped
      const next = { ...prev, multipliers }
      saveToStorage(next)
      return next
    })
  }, [])

  const resetToDefaults = useCallback(() => {
    update(defaultConfig())
  }, [update])

  return { config, setBaseSize, setMultiplier, resetToDefaults }
}

// ---------------------------------------------------------------------------
// Tier assignment helpers (used by GraphCanvas to compute __size per node)
// ---------------------------------------------------------------------------

/**
 * Compute a per-node degree percentile rank (0–100).
 *
 * Accepts the full sorted-ascending degree array and returns a function
 * that maps any degree value to its percentile.
 */
export function buildDegreePercentileFn(
  sortedDegrees: number[],
): (degree: number) => number {
  const n = sortedDegrees.length
  if (n === 0) return () => 0

  return (degree: number): number => {
    // Binary search for leftmost index where sortedDegrees[i] >= degree
    let lo = 0
    let hi = n
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (sortedDegrees[mid] < degree) lo = mid + 1
      else hi = mid
    }
    // lo = count of elements strictly less than degree
    return (lo / n) * 100
  }
}

/**
 * Map a percentile (0–100) to a tier index (0–5).
 */
export function percentileToTier(percentile: number): number {
  for (let i = 0; i < TIER_UPPER_PERCENTILES.length; i++) {
    if (percentile <= TIER_UPPER_PERCENTILES[i]) return i
  }
  return TIER_COUNT - 1
}

/**
 * Compute the final node size: base × multipliers[tier].
 */
export function computeTunedSize(
  degree: number,
  getPercentile: (degree: number) => number,
  config: NodeSizingConfig,
): number {
  const pct = getPercentile(degree)
  const tier = percentileToTier(pct)
  return config.baseSize * (config.multipliers[tier] ?? 1.0)
}
