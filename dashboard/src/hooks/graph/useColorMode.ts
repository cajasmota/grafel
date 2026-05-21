/**
 * useColorMode — persisted 3-way color mode for the graph.
 *
 * Modes:
 *   'repo'      — per-repo color (default, existing behavior)
 *   'degree'    — Cosmograph's built-in 'connections count' strategy
 *                 renders the Silk Road purple→pink→yellow gradient
 *   'community' — community_id with deterministic palette (#1153)
 *
 * Persisted to localStorage so the user's choice survives page reload.
 */

import { useState, useCallback } from 'react'

export type ColorMode = 'repo' | 'degree' | 'community'

const STORAGE_KEY = 'archigraph.graph.colorMode'
const VALID: ColorMode[] = ['repo', 'degree', 'community']

function readStored(): ColorMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw && VALID.includes(raw as ColorMode)) return raw as ColorMode
  } catch {
    // Private browsing — fall through to default
  }
  return 'repo'
}

export interface UseColorModeReturn {
  colorMode: ColorMode
  setColorMode: (mode: ColorMode) => void
}

export function useColorMode(): UseColorModeReturn {
  const [colorMode, setColorModeState] = useState<ColorMode>(readStored)

  const setColorMode = useCallback((mode: ColorMode) => {
    setColorModeState(mode)
    try {
      localStorage.setItem(STORAGE_KEY, mode)
    } catch {
      // Ignore quota / private-mode errors
    }
  }, [])

  return { colorMode, setColorMode }
}
