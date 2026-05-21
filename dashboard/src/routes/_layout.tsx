import { Link, Outlet, useParams } from 'react-router-dom'
import { useCallback, useEffect, useState } from 'react'
import { GitBranch, Moon, Search, Sun } from 'lucide-react'
import { useRegistry } from '@/hooks/shared/useRegistry'
import { useThemeContext } from '@/context/ThemeContext'
import { GroupSelector } from '@/components/layout/GroupSelector'
import { VersionPopover } from '@/components/layout/VersionPopover'
import { CommandPalette } from '@/components/layout/CommandPalette'
import {
  NavMenu,
  exploreItems,
  operateItems,
  useIsGroupActive,
} from '@/components/layout/NavMenu'

const GROUP_DEFAULT = 'fixture-a'

const EXPLORE_PREFIXES = ['/graph/', '/flows/', '/topology/', '/paths/', '/docs/', '/pending/']
const OPERATE_PREFIXES = ['/diagnostics', '/quality', '/patterns/', '/system', '/update', '/mcp-activity', '/settings']

export function AppLayout() {
  const { group = GROUP_DEFAULT } = useParams()
  const { data: registry } = useRegistry()
  const groups = registry?.groups ?? []

  const exploreActive = useIsGroupActive(EXPLORE_PREFIXES)
  const operateActive = useIsGroupActive(OPERATE_PREFIXES)

  const [paletteOpen, setPaletteOpen] = useState(false)
  const openPalette  = useCallback(() => setPaletteOpen(true),  [])
  const closePalette = useCallback(() => setPaletteOpen(false), [])

  // Keyboard shortcut: Cmd+K (Mac) / Ctrl+K (Linux/Win)
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setPaletteOpen((prev) => !prev)
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  // Listen for theme-toggle event dispatched by the CommandPalette action
  const { toggle: toggleTheme } = useThemeContext()
  useEffect(() => {
    document.addEventListener('archigraph:toggle-theme', toggleTheme)
    return () => document.removeEventListener('archigraph:toggle-theme', toggleTheme)
  }, [toggleTheme])

  // Keyboard shortcut: g h → go home
  useEffect(() => {
    let lastG = false
    const handler = (e: KeyboardEvent) => {
      // Don't fire shortcuts when the command palette is open or user is typing
      if (paletteOpen) return
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
      if (e.key === 'g' || e.key === 'G') {
        lastG = true
        setTimeout(() => { lastG = false }, 1000)
        return
      }
      if (lastG && (e.key === 'h' || e.key === 'H')) {
        e.preventDefault()
        window.location.href = '/'
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [paletteOpen])

  return (
    <div className="flex flex-col h-screen bg-white dark:bg-slate-950 text-slate-900 dark:text-slate-200">
      {/* ── Top nav ──────────────────────────────────────────────────────────── */}
      <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-200 dark:border-slate-800 flex-shrink-0 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm z-20">
        {/* Logo */}
        <Link to="/" className="flex items-center gap-2 font-bold text-sm tracking-tight text-sky-400 hover:text-sky-300">
          <GitBranch className="w-5 h-5" aria-hidden />
          archigraph
        </Link>

        {/* Surface nav — 2 grouped dropdowns (Explore / Operate) */}
        <nav className="flex items-center gap-1 ml-2 sm:ml-4 flex-shrink-0" aria-label="Surface navigation">
          <NavMenu
            label="Explore"
            testId="nav-explore"
            items={exploreItems(group)}
            isGroupActive={exploreActive}
          />
          <NavMenu
            label="Operate"
            testId="nav-operate"
            items={operateItems(group)}
            isGroupActive={operateActive}
          />
        </nav>

        <div className="ml-auto flex items-center gap-2">
          {/* Cmd+K chip — visible on mobile and as a quick-access button */}
          <button
            type="button"
            aria-label="Open command palette (⌘K)"
            title="Open command palette (⌘K)"
            data-testid="cmd-palette-chip"
            className={[
              'flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs',
              'text-slate-500 dark:text-slate-400',
              'bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700',
              'border border-slate-200 dark:border-slate-700',
              'transition-colors',
            ].join(' ')}
            onClick={openPalette}
          >
            <Search className="w-3 h-3" aria-hidden />
            <span className="hidden sm:inline">Search</span>
            <kbd className="hidden md:inline-flex items-center font-mono text-[10px] text-slate-400 dark:text-slate-500">⌘K</kbd>
          </button>

          {/* Group selector — sits between theme toggle and version info */}
          <GroupSelector groups={groups} />
          <ThemeToggle />
          <VersionPopover />
        </div>
      </header>

      {/* ── Body: full-width content (sidebar removed) ───────────────────────── */}
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>

      {/* ── Command Palette ───────────────────────────────────────────────────── */}
      <CommandPalette open={paletteOpen} onClose={closePalette} group={group} />
    </div>
  )
}

function ThemeToggle() {
  const { isDark, toggle } = useThemeContext()

  return (
    <button
      type="button"
      aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
      className="p-1.5 rounded text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
      onClick={toggle}
    >
      {isDark ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
    </button>
  )
}
