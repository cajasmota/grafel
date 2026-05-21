/**
 * useSimulationConfig — tunable force-simulation params with localStorage persistence.
 *
 * Exposes sliders for the five Cosmograph simulation knobs so the user can
 * dial in the right galaxy aesthetic live (#1361).
 *
 * Two built-in presets:
 *   'silk-road' — current Silk Road defaults (#1153)
 *   'dense'     — tighter graph: higher gravity, smaller spaceSize
 *
 * Persisted to localStorage so config survives page reload.
 */

import { useState, useCallback, useMemo } from 'react'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface SimulationConfig {
  spaceSize:    number   // 1024–16384
  gravity:      number   // 0.0–1.0
  linkSpring:   number   // 0.0–2.0
  linkDistance: number   // 1–50
  friction:     number   // 0.5–0.95
  repulsion:    number   // 0.1–10.0
  center:       number   // 0.0–1.0
  /**
   * settleTime — wall-clock cap (seconds) for the explode/settle animation on
   * a FRESH layout (no cached positions). GraphCanvas force-calls doSettle()
   * after this many seconds even if onSimulationEnd never fired, so the
   * initial → explode → settle animation never runs longer than this.
   * Default 2.0s, range 0.5–6s. Ignored when a saved layout is restored
   * (the simulation is skipped entirely in that case).
   */
  settleTime:   number   // 0.5–6.0 (seconds)
}

export type SimulationPreset = 'silk-road' | 'dense'

export interface SliderMeta {
  key:   keyof SimulationConfig
  label: string
  min:   number
  max:   number
  step:  number
}

// ---------------------------------------------------------------------------
// Presets
// ---------------------------------------------------------------------------

export const SILK_ROAD_DEFAULTS: SimulationConfig = {
  spaceSize:    4096,
  gravity:      0.46,
  linkSpring:   0.08,
  linkDistance: 2,
  friction:     0.77,
  repulsion:    4.0,
  center:       0.15,
  settleTime:   2.0,
}

export const DENSE_DEFAULTS: SimulationConfig = {
  spaceSize:    1024,
  gravity:      0.85,
  linkSpring:   0.5,
  linkDistance: 1,
  friction:     0.88,
  repulsion:    2.0,
  center:       0.3,
  settleTime:   2.0,
}

export const PRESET_CONFIGS: Record<SimulationPreset, SimulationConfig> = {
  'silk-road': SILK_ROAD_DEFAULTS,
  'dense':     DENSE_DEFAULTS,
}

// ---------------------------------------------------------------------------
// Slider metadata
// ---------------------------------------------------------------------------

export const SLIDER_META: SliderMeta[] = [
  { key: 'spaceSize',    label: 'Space Size',     min: 1024,  max: 16384, step: 256  },
  { key: 'gravity',      label: 'Gravity',        min: 0.0,   max: 1.0,   step: 0.01 },
  { key: 'repulsion',    label: 'Repulsion',      min: 0.1,   max: 10.0,  step: 0.1  },
  { key: 'center',       label: 'Center Force',   min: 0.0,   max: 1.0,   step: 0.01 },
  { key: 'linkSpring',   label: 'Link Spring',    min: 0.0,   max: 2.0,   step: 0.01 },
  { key: 'linkDistance', label: 'Link Distance',  min: 1,     max: 50,    step: 1    },
  { key: 'friction',     label: 'Friction',       min: 0.50,  max: 0.95,  step: 0.01 },
  { key: 'settleTime',   label: 'Settle time (s)', min: 0.5,  max: 6.0,   step: 0.1  },
]

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

const STORAGE_KEY = 'archigraph.graph.simulationConfig'
const URL_HASH_KEY = 'simcfg'

function encodeToHash(cfg: SimulationConfig): string {
  const params = new URLSearchParams({
    ss:  String(cfg.spaceSize),
    g:   String(cfg.gravity),
    ls:  String(cfg.linkSpring),
    ld:  String(cfg.linkDistance),
    fr:  String(cfg.friction),
    rp:  String(cfg.repulsion),
    ct:  String(cfg.center),
    st:  String(cfg.settleTime),
  })
  return params.toString()
}

function decodeFromHash(hash: string): Partial<SimulationConfig> {
  try {
    const params = new URLSearchParams(hash)
    const out: Partial<SimulationConfig> = {}
    const ss = Number(params.get('ss'));  if (isFinite(ss) && ss > 0) out.spaceSize    = ss
    const g  = Number(params.get('g'));   if (isFinite(g))            out.gravity      = g
    const ls = Number(params.get('ls'));  if (isFinite(ls))            out.linkSpring   = ls
    const ld = Number(params.get('ld'));  if (isFinite(ld) && ld > 0) out.linkDistance = ld
    const fr = Number(params.get('fr'));  if (isFinite(fr))            out.friction     = fr
    const rp = Number(params.get('rp'));  if (isFinite(rp) && rp > 0) out.repulsion    = rp
    const ct = Number(params.get('ct'));  if (isFinite(ct))            out.center       = ct
    const st = Number(params.get('st'));  if (isFinite(st) && st > 0) out.settleTime   = st
    return out
  } catch {
    return {}
  }
}

function readHashConfig(): Partial<SimulationConfig> {
  if (typeof window === 'undefined') return {}
  const hash = window.location.hash
  if (!hash.includes(URL_HASH_KEY + '=')) return {}
  try {
    const hashParams = new URLSearchParams(hash.slice(1))
    const encoded = hashParams.get(URL_HASH_KEY)
    if (!encoded) return {}
    return decodeFromHash(encoded)
  } catch {
    return {}
  }
}

function readStoredConfig(): SimulationConfig {
  // URL hash takes precedence over localStorage
  const hashCfg = readHashConfig()
  if (Object.keys(hashCfg).length > 0) {
    return { ...SILK_ROAD_DEFAULTS, ...hashCfg }
  }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<SimulationConfig>
      return { ...SILK_ROAD_DEFAULTS, ...parsed }
    }
  } catch {
    // Fall through to default
  }
  return SILK_ROAD_DEFAULTS
}

function persist(cfg: SimulationConfig): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
  } catch {
    // Ignore quota / private-mode errors
  }
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseSimulationConfigReturn {
  config:     SimulationConfig
  setParam:   (key: keyof SimulationConfig, value: number) => void
  applyPreset:(preset: SimulationPreset) => void
  /** URL-safe hash fragment containing current config (for "share" link) */
  shareHash:  string
}

export function useSimulationConfig(): UseSimulationConfigReturn {
  const [config, setConfig] = useState<SimulationConfig>(readStoredConfig)

  const setParam = useCallback((key: keyof SimulationConfig, value: number) => {
    setConfig((prev) => {
      const next = { ...prev, [key]: value }
      persist(next)
      return next
    })
  }, [])

  const applyPreset = useCallback((preset: SimulationPreset) => {
    const next = PRESET_CONFIGS[preset]
    setConfig(next)
    persist(next)
  }, [])

  const shareHash = useMemo(() => {
    const encoded = encodeToHash(config)
    return `#${URL_HASH_KEY}=${encodeURIComponent(encoded)}`
  }, [config])

  return { config, setParam, applyPreset, shareHash }
}
