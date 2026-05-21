import { createContext, useContext, ReactNode, useState, useEffect, useCallback } from 'react'
import { useThemePreset } from '@/hooks/useThemePreset'
import type { UseThemePresetReturn } from '@/hooks/useThemePreset'

type Theme = 'dark' | 'light'

interface ThemeContextType {
  theme: Theme
  isDark: boolean
  toggle: () => void
  // Preset & custom palette support
  preset: UseThemePresetReturn['preset']
  palette: UseThemePresetReturn['palette']
  setPreset: UseThemePresetReturn['setPreset']
  setPalette: UseThemePresetReturn['setPalette']
  exportPaletteJSON: UseThemePresetReturn['exportPaletteJSON']
  importPaletteJSON: UseThemePresetReturn['importPaletteJSON']
  resetPalette: UseThemePresetReturn['resetPalette']
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined)

function getInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'light'
  const stored = localStorage.getItem('theme')
  if (stored === 'dark' || stored === 'light') return stored
  // Default is light — no stored preference means first-time visitor gets light
  return 'light'
}

function applyTheme(theme: Theme) {
  const html = document.documentElement
  if (theme === 'dark') {
    html.classList.add('dark')
  } else {
    html.classList.remove('dark')
  }
  localStorage.setItem('theme', theme)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => {
    const initial = getInitialTheme()
    // Apply synchronously so first paint matches the stored/system preference.
    applyTheme(initial)
    return initial
  })

  const presetHook = useThemePreset()

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((prev) => (prev === 'dark' ? 'light' : 'dark'))
  }, [])

  return (
    <ThemeContext.Provider
      value={{
        theme,
        toggle,
        isDark: theme === 'dark',
        preset: presetHook.preset,
        palette: presetHook.palette,
        setPreset: presetHook.setPreset,
        setPalette: presetHook.setPalette,
        exportPaletteJSON: presetHook.exportPaletteJSON,
        importPaletteJSON: presetHook.importPaletteJSON,
        resetPalette: presetHook.resetPalette,
      }}
    >
      {children}
    </ThemeContext.Provider>
  )
}

export function useThemeContext() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useThemeContext must be used within a ThemeProvider')
  }
  return context
}
