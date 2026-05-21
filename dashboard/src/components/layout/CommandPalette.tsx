/**
 * CommandPalette — Cmd+K (Mac) / Ctrl+K (Linux/Win) global command palette.
 *
 * Features:
 *   - Fuzzy search over all surfaces + actions using fuse.js (already in deps)
 *   - Two sections: Surfaces and Actions
 *   - Arrow-key navigation, Enter to select, Esc to close
 *   - Recent items (up to 5) stored in localStorage, shown when input empty
 *   - Dimmed backdrop, smooth fade-in animation
 *   - Mobile: accessible via data-testid="cmd-palette-chip" chip in header
 *
 * Closes: #1234
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Fuse from 'fuse.js'
import {
  Network, Workflow, Radio, Globe, BookOpen, Clock,
  Stethoscope, BarChart2, Sparkles, Server, RefreshCw,
  Settings, Activity, Wrench, RotateCcw, Sun, Moon,
  RefreshCcw, Home, HelpCircle,
} from 'lucide-react'

/* ── Types ──────────────────────────────────────────────────────────────────── */

type CommandKind = 'surface' | 'action'

interface Command {
  id: string
  label: string
  description?: string
  kind: CommandKind
  /** For surfaces: navigate to this path */
  to?: string
  /** For actions: call this function */
  fn?: () => void
  icon: React.ReactNode
  /** Coming soon — not wired yet */
  comingSoon?: boolean
}

/* ── Constants ──────────────────────────────────────────────────────────────── */

const RECENT_KEY = 'archigraph:cmd-recent'
const MAX_RECENT = 5

function loadRecent(): string[] {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]')
  } catch {
    return []
  }
}

function saveRecent(id: string) {
  const prev = loadRecent().filter((x) => x !== id)
  localStorage.setItem(RECENT_KEY, JSON.stringify([id, ...prev].slice(0, MAX_RECENT)))
}

/* ── CommandPalette component ───────────────────────────────────────────────── */

interface CommandPaletteProps {
  open: boolean
  onClose: () => void
  /** Active group slug for group-scoped surface links */
  group?: string
}

export function CommandPalette({ open, onClose, group = 'fixture-a' }: CommandPaletteProps) {
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)

  /* ── Build command list ──────────────────────────────────────────────────── */

  const commands = useMemo((): Command[] => [
    // Surfaces
    { id: 'surface-graph',       label: 'Graph',       description: 'Dependency graph view',   kind: 'surface', to: `/graph/${group}`,       icon: <Network    className="w-4 h-4" /> },
    { id: 'surface-flows',       label: 'Flows',       description: 'Execution flow traces',   kind: 'surface', to: `/flows/${group}`,       icon: <Workflow   className="w-4 h-4" /> },
    { id: 'surface-topology',    label: 'Topology',    description: 'Service topology map',    kind: 'surface', to: `/topology/${group}`,    icon: <Radio      className="w-4 h-4" /> },
    { id: 'surface-paths',       label: 'API Explorer', description: 'Browse all API paths',  kind: 'surface', to: `/paths/${group}`,       icon: <Globe      className="w-4 h-4" /> },
    { id: 'surface-docs',        label: 'Docs',        description: 'Auto-generated docs',     kind: 'surface', to: `/docs/${group}`,        icon: <BookOpen   className="w-4 h-4" /> },
    { id: 'surface-pending',     label: 'Pending',     description: 'Repair + enrichment queue', kind: 'surface', to: `/pending/${group}`,  icon: <Clock      className="w-4 h-4" /> },
    { id: 'surface-diagnostics', label: 'Diagnostics', description: 'Health & error report',   kind: 'surface', to: '/diagnostics',          icon: <Stethoscope className="w-4 h-4" /> },
    { id: 'surface-quality',     label: 'Quality',     description: 'Health-score history',    kind: 'surface', to: `/quality/${group}`,     icon: <BarChart2  className="w-4 h-4" /> },
    { id: 'surface-patterns',    label: 'Patterns',    description: 'Detected code patterns',  kind: 'surface', to: `/patterns/${group}`,   icon: <Sparkles   className="w-4 h-4" /> },
    { id: 'surface-system',      label: 'System',      description: 'Daemon control panel',    kind: 'surface', to: '/system',               icon: <Server     className="w-4 h-4" /> },
    { id: 'surface-update',      label: 'Update',      description: 'Version management',      kind: 'surface', to: '/update',               icon: <RefreshCw  className="w-4 h-4" /> },
    { id: 'surface-settings',    label: 'Settings',    description: 'App configuration',       kind: 'surface', to: '/settings',             icon: <Settings   className="w-4 h-4" /> },
    { id: 'surface-mcp-activity', label: 'MCP Activity', description: 'MCP tool call log',   kind: 'surface', to: '/mcp-activity',          icon: <Activity   className="w-4 h-4" /> },
    { id: 'surface-home',        label: 'Home',        description: 'Dashboard home',          kind: 'surface', to: '/',                     icon: <Home       className="w-4 h-4" /> },

    // Actions
    {
      id: 'action-toggle-theme',
      label: 'Toggle theme',
      description: 'Switch between light and dark mode',
      kind: 'action',
      icon: <Sun className="w-4 h-4" />,
      fn: () => {
        // Dispatch a custom event — _layout.tsx ThemeToggle handles it
        document.dispatchEvent(new CustomEvent('archigraph:toggle-theme'))
      },
    },
    {
      id: 'action-open-settings',
      label: 'Open Settings',
      description: 'Go to Settings page',
      kind: 'action',
      to: '/settings',
      icon: <Settings className="w-4 h-4" />,
    },
    {
      id: 'action-rebuild',
      label: 'Rebuild this group',
      description: 'Trigger a full re-index of the current group',
      kind: 'action',
      icon: <Wrench className="w-4 h-4" />,
      comingSoon: true,
      fn: () => { /* coming soon */ },
    },
    {
      id: 'action-reset',
      label: 'Reset layout',
      description: 'Reset graph/topology layout to default',
      kind: 'action',
      icon: <RotateCcw className="w-4 h-4" />,
      comingSoon: true,
      fn: () => { /* coming soon */ },
    },
    {
      id: 'action-cleanup',
      label: 'Run cleanup',
      description: 'Remove stale nodes from the index',
      kind: 'action',
      icon: <RefreshCcw className="w-4 h-4" />,
      comingSoon: true,
      fn: () => { /* coming soon */ },
    },
    {
      id: 'action-restart-daemon',
      label: 'Restart daemon',
      description: 'Graceful daemon restart',
      kind: 'action',
      icon: <Server className="w-4 h-4" />,
      comingSoon: true,
      fn: () => { /* coming soon */ },
    },
    {
      id: 'action-check-updates',
      label: 'Check for updates',
      description: 'Check for a newer archigraph version',
      kind: 'action',
      to: '/update',
      icon: <RefreshCw className="w-4 h-4" />,
    },
    {
      id: 'action-keyboard-shortcuts',
      label: 'Keyboard shortcuts',
      description: 'View all keyboard shortcuts (? overlay)',
      kind: 'action',
      icon: <HelpCircle className="w-4 h-4" />,
      comingSoon: true,
      fn: () => { /* coming soon */ },
    },
  ], [group])

  /* ── Fuzzy search ─────────────────────────────────────────────────────────── */

  const fuse = useMemo(() => new Fuse(commands, {
    keys: ['label', 'description'],
    threshold: 0.35,
    includeScore: true,
  }), [commands])

  const recent = useMemo(() => loadRecent(), [open]) // eslint-disable-line react-hooks/exhaustive-deps

  const filtered = useMemo((): Command[] => {
    if (!query.trim()) {
      // Show recent items first, then all
      const recentCmds = recent
        .map((id) => commands.find((c) => c.id === id))
        .filter(Boolean) as Command[]
      const rest = commands.filter((c) => !recent.includes(c.id))
      return [...recentCmds, ...rest]
    }
    return fuse.search(query).map((r) => r.item)
  }, [query, fuse, commands, recent])

  const surfaces = filtered.filter((c) => c.kind === 'surface')
  const actions  = filtered.filter((c) => c.kind === 'action')

  const allVisible = [...surfaces, ...actions]

  /* ── Reset state when opened ─────────────────────────────────────────────── */

  useEffect(() => {
    if (open) {
      setQuery('')
      setActiveIndex(0)
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  /* ── Keyboard navigation ─────────────────────────────────────────────────── */

  const execute = useCallback((cmd: Command) => {
    saveRecent(cmd.id)
    onClose()
    if (cmd.to) {
      navigate(cmd.to)
    } else if (cmd.fn && !cmd.comingSoon) {
      cmd.fn()
    }
  }, [navigate, onClose])

  useEffect(() => {
    if (!open) return

    const handler = (e: KeyboardEvent) => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault()
          setActiveIndex((i) => Math.min(i + 1, allVisible.length - 1))
          break
        case 'ArrowUp':
          e.preventDefault()
          setActiveIndex((i) => Math.max(i - 1, 0))
          break
        case 'Enter': {
          e.preventDefault()
          const cmd = allVisible[activeIndex]
          if (cmd) execute(cmd)
          break
        }
        case 'Escape':
          e.preventDefault()
          onClose()
          break
      }
    }

    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open, allVisible, activeIndex, execute, onClose])

  /* ── Scroll active item into view ────────────────────────────────────────── */

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-active="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  /* ── Reset activeIndex on query change ───────────────────────────────────── */

  useEffect(() => {
    setActiveIndex(0)
  }, [query])

  if (!open) return null

  /* ── Render ──────────────────────────────────────────────────────────────── */

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
      data-testid="cmd-palette-backdrop"
      onClick={onClose}
    >
      {/* Dimmed backdrop */}
      <div className="absolute inset-0 bg-black/50 animate-in fade-in-0 duration-150" />

      {/* Palette container */}
      <div
        className={[
          'relative w-full max-w-lg mx-4 rounded-xl border shadow-2xl overflow-hidden',
          'bg-white dark:bg-slate-900',
          'border-slate-200 dark:border-slate-700',
          'animate-in fade-in-0 zoom-in-95 duration-150',
        ].join(' ')}
        data-testid="cmd-palette"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
      >
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-700">
          <svg
            className="w-4 h-4 text-slate-400 flex-shrink-0"
            fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}
            aria-hidden
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-4.35-4.35M17 11A6 6 0 1 1 5 11a6 6 0 0 1 12 0z" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search surfaces and actions…"
            className={[
              'flex-1 bg-transparent text-sm outline-none',
              'text-slate-900 dark:text-slate-100',
              'placeholder:text-slate-400 dark:placeholder:text-slate-500',
            ].join(' ')}
            data-testid="cmd-palette-input"
            aria-label="Search command palette"
            autoComplete="off"
            spellCheck={false}
          />
          <kbd className="hidden sm:inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded text-xs font-mono text-slate-400 dark:text-slate-500 border border-slate-200 dark:border-slate-700">
            esc
          </kbd>
        </div>

        {/* Results list */}
        <div
          ref={listRef}
          className="max-h-[60vh] overflow-y-auto py-2"
          role="listbox"
          aria-label="Commands"
        >
          {allVisible.length === 0 && (
            <p className="px-4 py-6 text-center text-sm text-slate-400 dark:text-slate-500">
              No results for &ldquo;{query}&rdquo;
            </p>
          )}

          {/* Surfaces section */}
          {surfaces.length > 0 && (
            <Section
              heading={query ? 'Surfaces' : recent.length > 0 ? 'Recent & Surfaces' : 'Surfaces'}
              commands={surfaces}
              offset={0}
              activeIndex={activeIndex}
              onSelect={execute}
              onHover={(idx) => setActiveIndex(idx)}
            />
          )}

          {/* Actions section */}
          {actions.length > 0 && (
            <Section
              heading="Actions"
              commands={actions}
              offset={surfaces.length}
              activeIndex={activeIndex}
              onSelect={execute}
              onHover={(idx) => setActiveIndex(idx)}
            />
          )}
        </div>

        {/* Footer hint */}
        <div className="flex items-center gap-3 px-4 py-2 border-t border-slate-100 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-500">
          <span><kbd className="font-mono">↑↓</kbd> navigate</span>
          <span><kbd className="font-mono">↵</kbd> open</span>
          <span><kbd className="font-mono">esc</kbd> close</span>
        </div>
      </div>
    </div>
  )
}

/* ── Section ────────────────────────────────────────────────────────────────── */

interface SectionProps {
  heading: string
  commands: Command[]
  offset: number
  activeIndex: number
  onSelect: (cmd: Command) => void
  onHover: (idx: number) => void
}

function Section({ heading, commands, offset, activeIndex, onSelect, onHover }: SectionProps) {
  return (
    <div>
      <p className="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
        {heading}
      </p>
      {commands.map((cmd, i) => {
        const idx = offset + i
        const isActive = idx === activeIndex
        return (
          <CommandRow
            key={cmd.id}
            cmd={cmd}
            isActive={isActive}
            onSelect={onSelect}
            onHover={() => onHover(idx)}
          />
        )
      })}
    </div>
  )
}

/* ── CommandRow ─────────────────────────────────────────────────────────────── */

interface CommandRowProps {
  cmd: Command
  isActive: boolean
  onSelect: (cmd: Command) => void
  onHover: () => void
}

function CommandRow({ cmd, isActive, onSelect, onHover }: CommandRowProps) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={isActive}
      data-active={isActive}
      data-testid={`cmd-item-${cmd.id}`}
      className={[
        'w-full flex items-center gap-3 px-3 py-2 text-sm transition-colors text-left',
        isActive
          ? 'bg-sky-50 dark:bg-sky-950/40 text-sky-700 dark:text-sky-300'
          : 'text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800/50',
        cmd.comingSoon ? 'opacity-60' : '',
      ].join(' ')}
      onClick={() => onSelect(cmd)}
      onMouseEnter={onHover}
    >
      <span className="flex-shrink-0 text-slate-400 dark:text-slate-500">{cmd.icon}</span>
      <span className="flex-1 min-w-0">
        <span className="font-medium">{cmd.label}</span>
        {cmd.description && (
          <span className="ml-2 text-xs text-slate-400 dark:text-slate-500 truncate">
            {cmd.description}
          </span>
        )}
      </span>
      {cmd.comingSoon && (
        <span className="flex-shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500">
          soon
        </span>
      )}
    </button>
  )
}
