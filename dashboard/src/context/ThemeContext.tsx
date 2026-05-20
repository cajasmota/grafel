import { createContext, useContext, ReactNode, useState, useEffect, useCallback } from 'react'

type Theme = 'dark' | 'light'

interface ThemeContextType {
  theme: Theme
  isDark: boolean
  toggle: () => void
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined)

function getInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'dark'
  const stored = localStorage.getItem('theme')
  if (stored === 'dark' || stored === 'light') return stored
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
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
    // main.tsx already does this before React boots, but ThemeProvider guards
    // against the edge-case where main.tsx runs before localStorage is ready.
    applyTheme(initial)
    return initial
  })

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((prev) => (prev === 'dark' ? 'light' : 'dark'))
  }, [])

  return (
    <ThemeContext.Provider value={{ theme, toggle, isDark: theme === 'dark' }}>
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
