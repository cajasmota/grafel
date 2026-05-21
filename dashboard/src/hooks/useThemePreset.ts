/**
 * useThemePreset — manages the active colour preset and custom palette.
 *
 * Applies a CSS class on <html> for built-in presets (e.g. "theme-nord").
 * For the custom preset it injects inline CSS custom properties onto <html>.
 * Persists choice in localStorage so it survives hard refresh.
 */
import { useState, useEffect, useCallback } from 'react'
import type { ThemePreset, CustomPalette } from '@/api/settings'
import { DEFAULT_CUSTOM_PALETTE } from '@/api/settings'

const LS_PRESET_KEY = 'ag-theme-preset'
const LS_CUSTOM_KEY = 'ag-theme-custom'

/** All preset class names that may be applied to <html>. */
const PRESET_CLASSES: ThemePreset[] = [
  'solarized-dark',
  'nord',
  'catppuccin-mocha',
  'high-contrast',
  'custom',
]

function loadPreset(): ThemePreset {
  try {
    const v = localStorage.getItem(LS_PRESET_KEY)
    if (
      v === 'default' ||
      v === 'solarized-dark' ||
      v === 'nord' ||
      v === 'catppuccin-mocha' ||
      v === 'high-contrast' ||
      v === 'custom'
    ) return v as ThemePreset
  } catch { /* ignore */ }
  return 'default'
}

function loadCustomPalette(): CustomPalette {
  try {
    const raw = localStorage.getItem(LS_CUSTOM_KEY)
    if (raw) return { ...DEFAULT_CUSTOM_PALETTE, ...JSON.parse(raw) }
  } catch { /* ignore */ }
  return { ...DEFAULT_CUSTOM_PALETTE }
}

function applyPreset(preset: ThemePreset, palette: CustomPalette) {
  const html = document.documentElement

  // Strip previous preset classes
  for (const p of PRESET_CLASSES) {
    html.classList.remove(`theme-${p}`)
  }

  // Remove any previously injected custom vars
  const varNames = ['bg','bg-card','bg-input','fg','fg-muted','border','accent','accent-fg']
  for (const v of varNames) {
    html.style.removeProperty(`--ag-${v}`)
  }

  if (preset === 'default') return

  if (preset === 'custom') {
    html.classList.add('theme-custom')
    // Inject palette as inline custom properties
    html.style.setProperty('--ag-bg',        palette.bg)
    html.style.setProperty('--ag-bg-card',   palette.bg_card)
    html.style.setProperty('--ag-bg-input',  palette.bg_input)
    html.style.setProperty('--ag-fg',        palette.fg)
    html.style.setProperty('--ag-fg-muted',  palette.fg_muted)
    html.style.setProperty('--ag-border',    palette.border)
    html.style.setProperty('--ag-accent',    palette.accent)
    html.style.setProperty('--ag-accent-fg', palette.accent_fg)
  } else {
    html.classList.add(`theme-${preset}`)
  }
}

export interface UseThemePresetReturn {
  preset: ThemePreset
  palette: CustomPalette
  setPreset: (p: ThemePreset) => void
  setPalette: (patch: Partial<CustomPalette>) => void
  exportPaletteJSON: () => string
  importPaletteJSON: (json: string) => { ok: boolean; error?: string }
  resetPalette: () => void
}

export function useThemePreset(): UseThemePresetReturn {
  const [preset, setPresetState] = useState<ThemePreset>(loadPreset)
  const [palette, setPaletteState] = useState<CustomPalette>(loadCustomPalette)

  // Apply on mount and whenever preset/palette change
  useEffect(() => {
    applyPreset(preset, palette)
    try { localStorage.setItem(LS_PRESET_KEY, preset) } catch { /* ignore */ }
  }, [preset, palette])

  const setPreset = useCallback((p: ThemePreset) => {
    setPresetState(p)
  }, [])

  const setPalette = useCallback((patch: Partial<CustomPalette>) => {
    setPaletteState((prev) => {
      const next = { ...prev, ...patch }
      try { localStorage.setItem(LS_CUSTOM_KEY, JSON.stringify(next)) } catch { /* ignore */ }
      return next
    })
  }, [])

  const exportPaletteJSON = useCallback((): string => {
    return JSON.stringify(palette, null, 2)
  }, [palette])

  const importPaletteJSON = useCallback((json: string): { ok: boolean; error?: string } => {
    try {
      const parsed = JSON.parse(json)
      const required = ['bg','bg_card','bg_input','fg','fg_muted','border','accent','accent_fg']
      for (const k of required) {
        if (typeof parsed[k] !== 'string') throw new Error(`Missing or invalid field: ${k}`)
      }
      setPalette(parsed as CustomPalette)
      return { ok: true }
    } catch (e) {
      return { ok: false, error: String(e) }
    }
  }, [setPalette])

  const resetPalette = useCallback(() => {
    setPaletteState({ ...DEFAULT_CUSTOM_PALETTE })
    try { localStorage.setItem(LS_CUSTOM_KEY, JSON.stringify(DEFAULT_CUSTOM_PALETTE)) } catch { /* ignore */ }
  }, [])

  return { preset, palette, setPreset, setPalette, exportPaletteJSON, importPaletteJSON, resetPalette }
}
