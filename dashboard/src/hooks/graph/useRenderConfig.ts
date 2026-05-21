/**
 * useRenderConfig — persisted tunable rendering config.
 *
 * Exposes live controls for cosmos.gl rendering knobs that were previously
 * hardcoded in GraphCanvas, causing whack-a-mole visual regressions.
 *
 * Persisted to localStorage under key `archigraph:graph:rendering`.
 * Changes apply immediately via setConfig() — no simulation restart.
 */
import { useState, useCallback } from 'react'

// ---------------------------------------------------------------------------
// Types + defaults
// ---------------------------------------------------------------------------

export interface RenderConfig {
  /** cosmos pointOpacity — overall node opacity. Range 0.05–1.0 */
  pointOpacity: number
  /** cosmos pointSizeScale — global multiplier on setPointSizes values. Range 0.05–2.0 */
  pointSizeScale: number
  /** cosmos scalePointsOnZoom — nodes grow with zoom level when true */
  scalePointsOnZoom: boolean
  /**
   * maxPointSize — soft clamp on rendered node size in screen-px.
   * cosmos.gl has no direct maxPointSize option; we enforce it by capping
   * pointSizeScale to: effectiveScale = min(pointSizeScale, maxPointSize / baseSize)
   * (see GraphCanvas applyRenderConfig). Range 4–200 px.
   */
  maxPointSize: number
  /**
   * linkOpacity — alpha channel applied to same-repo link colors (state 0).
   * Cross-repo (state 1) and highlighted (state 2) links keep their own alphas.
   * Range 0–1.
   */
  linkOpacity: number
  /** cosmos linkWidthScale — global multiplier on setLinkWidths values. Range 0.05–2.0 */
  linkWidthScale: number
  /** showLinks — hide all edges when false (linkWidthScale driven to 0) */
  showLinks: boolean
}

// Keep current hardcoded GraphCanvas values as defaults so nothing changes on
// first load until the owner explicitly tweaks a knob.
export const DEFAULT_RENDER_CONFIG: RenderConfig = {
  pointOpacity:      0.25,
  pointSizeScale:    0.22,
  scalePointsOnZoom: true,
  maxPointSize:      150,
  linkOpacity:       0.15,
  linkWidthScale:    0.16,
  showLinks:         true,
}

const STORAGE_KEY = 'archigraph:graph:rendering'

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

function loadFromStorage(): RenderConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const p = JSON.parse(raw) as Partial<RenderConfig>
    return {
      pointOpacity:      clampF(p.pointOpacity,   0.05, 1.0,   DEFAULT_RENDER_CONFIG.pointOpacity),
      pointSizeScale:    clampF(p.pointSizeScale,  0.05, 2.0,   DEFAULT_RENDER_CONFIG.pointSizeScale),
      scalePointsOnZoom: typeof p.scalePointsOnZoom === 'boolean' ? p.scalePointsOnZoom : DEFAULT_RENDER_CONFIG.scalePointsOnZoom,
      maxPointSize:      clampF(p.maxPointSize,    4,    200,   DEFAULT_RENDER_CONFIG.maxPointSize),
      linkOpacity:       clampF(p.linkOpacity,     0,    1.0,   DEFAULT_RENDER_CONFIG.linkOpacity),
      linkWidthScale:    clampF(p.linkWidthScale,  0.05, 2.0,   DEFAULT_RENDER_CONFIG.linkWidthScale),
      showLinks:         typeof p.showLinks === 'boolean' ? p.showLinks : DEFAULT_RENDER_CONFIG.showLinks,
    }
  } catch {
    return null
  }
}

function clampF(v: unknown, min: number, max: number, fallback: number): number {
  if (typeof v !== 'number' || !isFinite(v)) return fallback
  return Math.max(min, Math.min(max, v))
}

function saveToStorage(cfg: RenderConfig): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
  } catch {
    // Fail silently (localStorage unavailable / quota exceeded)
  }
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseRenderConfigReturn {
  config: RenderConfig
  setParam: <K extends keyof RenderConfig>(key: K, value: RenderConfig[K]) => void
  resetToDefaults: () => void
  /** True if any value differs from the defaults */
  isModified: boolean
}

export function useRenderConfig(): UseRenderConfigReturn {
  const [config, setConfig] = useState<RenderConfig>(
    () => loadFromStorage() ?? { ...DEFAULT_RENDER_CONFIG },
  )

  const setParam = useCallback(<K extends keyof RenderConfig>(key: K, value: RenderConfig[K]) => {
    setConfig((prev) => {
      const next = { ...prev, [key]: value }
      saveToStorage(next)
      return next
    })
  }, [])

  const resetToDefaults = useCallback(() => {
    const next = { ...DEFAULT_RENDER_CONFIG }
    setConfig(next)
    saveToStorage(next)
  }, [])

  const isModified = (
    config.pointOpacity      !== DEFAULT_RENDER_CONFIG.pointOpacity      ||
    config.pointSizeScale    !== DEFAULT_RENDER_CONFIG.pointSizeScale    ||
    config.scalePointsOnZoom !== DEFAULT_RENDER_CONFIG.scalePointsOnZoom ||
    config.maxPointSize      !== DEFAULT_RENDER_CONFIG.maxPointSize      ||
    config.linkOpacity       !== DEFAULT_RENDER_CONFIG.linkOpacity       ||
    config.linkWidthScale    !== DEFAULT_RENDER_CONFIG.linkWidthScale    ||
    config.showLinks         !== DEFAULT_RENDER_CONFIG.showLinks
  )

  return { config, setParam, resetToDefaults, isModified }
}
