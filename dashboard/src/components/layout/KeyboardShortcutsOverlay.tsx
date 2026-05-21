/**
 * KeyboardShortcutsOverlay — Press '?' anywhere (outside input) to open.
 *
 * Features:
 *   - Categorised shortcut table with collapsible sections
 *   - Search input to filter shortcuts
 *   - OS-aware: shows ⌘ on macOS, Ctrl on Linux/Windows
 *   - Detects shortcut scope badge (Global, Graph only, etc.)
 *   - Esc closes
 *
 * Closes: #1245
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, ChevronRight, Keyboard, X } from 'lucide-react'

/* ── OS detection ───────────────────────────────────────────────────────────── */

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform)

function mod(key: string): string {
  return isMac ? `⌘${key}` : `Ctrl+${key}`
}

/* ── Shortcut data ──────────────────────────────────────────────────────────── */

export type ShortcutScope = 'Global' | 'Graph only' | 'Topology only' | 'Lists' | 'Modals' | 'Indexing'

export interface Shortcut {
  keys: string[]
  description: string
  scope: ShortcutScope
}

export interface ShortcutCategory {
  id: string
  label: string
  shortcuts: Shortcut[]
}

export function buildCategories(): ShortcutCategory[] {
  return [
    {
      id: 'global',
      label: 'Global',
      shortcuts: [
        { keys: [mod('K')],     description: 'Open command palette',        scope: 'Global' },
        { keys: ['?'],          description: 'Open keyboard shortcuts',      scope: 'Global' },
        { keys: ['g', 'h'],     description: 'Go to home',                   scope: 'Global' },
        { keys: ['Esc'],        description: 'Close overlay / modal',        scope: 'Global' },
      ],
    },
    {
      id: 'graph',
      label: 'Graph',
      shortcuts: [
        { keys: ['+'],              description: 'Zoom in',                         scope: 'Graph only' },
        { keys: ['-'],              description: 'Zoom out',                         scope: 'Graph only' },
        { keys: ['F'],              description: 'Fit graph to viewport',           scope: 'Graph only' },
        { keys: ['0'],              description: 'Reset zoom to 100%',              scope: 'Graph only' },
        { keys: ['L'],              description: 'Toggle legend',                   scope: 'Graph only' },
        { keys: ['C'],              description: 'Toggle cluster grouping',         scope: 'Graph only' },
        { keys: ['/'],              description: 'Focus search input',              scope: 'Graph only' },
        { keys: ['↑'],              description: 'Jump to highest-pagerank outbound neighbor', scope: 'Graph only' },
        { keys: ['↓'],              description: 'Jump to highest-pagerank inbound neighbor',  scope: 'Graph only' },
        { keys: ['←'],              description: 'Jump to alphabetically-previous neighbor',   scope: 'Graph only' },
        { keys: ['→'],              description: 'Jump to alphabetically-next neighbor',        scope: 'Graph only' },
        { keys: ['Tab'],            description: 'Cycle through neighbors (forward)',            scope: 'Graph only' },
        { keys: ['Shift+Tab'],      description: 'Cycle through neighbors (backward)',           scope: 'Graph only' },
        { keys: ['Enter'],          description: 'Open detail panel for selected node',          scope: 'Graph only' },
        { keys: ['N'],              description: 'Select next search match',                     scope: 'Graph only' },
        { keys: ['P'],              description: 'Select previous search match',                 scope: 'Graph only' },
        { keys: [mod('[')],         description: 'Navigate back in history',                     scope: 'Graph only' },
        { keys: [mod(']')],         description: 'Navigate forward in history',                  scope: 'Graph only' },
      ],
    },
    {
      id: 'topology',
      label: 'Topology',
      shortcuts: [
        { keys: ['+'],          description: 'Zoom in',                      scope: 'Topology only' },
        { keys: ['-'],          description: 'Zoom out',                     scope: 'Topology only' },
        { keys: ['F'],          description: 'Fit topology to viewport',     scope: 'Topology only' },
        { keys: ['0'],          description: 'Reset zoom',                   scope: 'Topology only' },
      ],
    },
    {
      id: 'lists',
      label: 'Lists & Tabs',
      shortcuts: [
        { keys: ['↑'],          description: 'Move selection up',            scope: 'Lists' },
        { keys: ['↓'],          description: 'Move selection down',          scope: 'Lists' },
        { keys: ['Enter'],      description: 'Open selected row',            scope: 'Lists' },
      ],
    },
    {
      id: 'modals',
      label: 'Modals',
      shortcuts: [
        { keys: ['Esc'],        description: 'Close modal',                  scope: 'Modals' },
        { keys: ['Enter'],      description: 'Confirm / submit',             scope: 'Modals' },
      ],
    },
    {
      id: 'indexing',
      label: 'Indexing',
      shortcuts: [
        { keys: ['Shift+R'],    description: 'Rebuild current group',        scope: 'Indexing' },
        { keys: ['Ctrl+R'],     description: 'Rebuild all groups',           scope: 'Indexing' },
      ],
    },
  ]
}

/* ── Scope badge colours ────────────────────────────────────────────────────── */

const SCOPE_STYLES: Record<ShortcutScope, string> = {
  Global:         'bg-sky-100 dark:bg-sky-950/60 text-sky-700 dark:text-sky-300',
  'Graph only':   'bg-violet-100 dark:bg-violet-950/60 text-violet-700 dark:text-violet-300',
  'Topology only':'bg-indigo-100 dark:bg-indigo-950/60 text-indigo-700 dark:text-indigo-300',
  Lists:          'bg-emerald-100 dark:bg-emerald-950/60 text-emerald-700 dark:text-emerald-300',
  Modals:         'bg-amber-100 dark:bg-amber-950/60 text-amber-700 dark:text-amber-300',
  Indexing:       'bg-rose-100 dark:bg-rose-950/60 text-rose-700 dark:text-rose-300',
}

/* ── KeyboardShortcutsOverlay ───────────────────────────────────────────────── */

interface KeyboardShortcutsOverlayProps {
  open: boolean
  onClose: () => void
}

export function KeyboardShortcutsOverlay({ open, onClose }: KeyboardShortcutsOverlayProps) {
  const [search, setSearch]           = useState('')
  const [collapsed, setCollapsed]     = useState<Record<string, boolean>>({})
  const searchRef                     = useRef<HTMLInputElement>(null)

  const categories = useMemo(() => buildCategories(), [])

  /* ── Filtered categories ─────────────────────────────────────────────────── */

  const filtered = useMemo((): ShortcutCategory[] => {
    const q = search.trim().toLowerCase()
    if (!q) return categories
    return categories
      .map((cat) => ({
        ...cat,
        shortcuts: cat.shortcuts.filter(
          (s) =>
            s.description.toLowerCase().includes(q) ||
            s.keys.some((k) => k.toLowerCase().includes(q)) ||
            s.scope.toLowerCase().includes(q),
        ),
      }))
      .filter((cat) => cat.shortcuts.length > 0)
  }, [search, categories])

  /* ── Toggle collapse ─────────────────────────────────────────────────────── */

  const toggleCollapse = useCallback((id: string) => {
    setCollapsed((prev) => ({ ...prev, [id]: !prev[id] }))
  }, [])

  /* ── Reset on open ───────────────────────────────────────────────────────── */

  useEffect(() => {
    if (open) {
      setSearch('')
      setCollapsed({})
      setTimeout(() => searchRef.current?.focus(), 0)
    }
  }, [open])

  /* ── Esc to close ────────────────────────────────────────────────────────── */

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open, onClose])

  if (!open) return null

  /* ── Render ──────────────────────────────────────────────────────────────── */

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh]"
      data-testid="shortcuts-overlay-backdrop"
      onClick={onClose}
    >
      {/* Dimmed backdrop */}
      <div className="absolute inset-0 bg-black/50 animate-in fade-in-0 duration-150" />

      {/* Modal container */}
      <div
        className={[
          'relative w-full max-w-2xl mx-4 rounded-xl border shadow-2xl overflow-hidden',
          'bg-white dark:bg-slate-900',
          'border-slate-200 dark:border-slate-700',
          'animate-in fade-in-0 zoom-in-95 duration-150',
          'flex flex-col max-h-[80vh]',
        ].join(' ')}
        data-testid="shortcuts-overlay"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Keyboard shortcuts"
      >
        {/* Header */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
          <Keyboard className="w-4 h-4 text-slate-400 flex-shrink-0" aria-hidden />
          <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 flex-1">
            Keyboard Shortcuts
          </h2>
          <button
            type="button"
            aria-label="Close shortcuts overlay"
            data-testid="shortcuts-overlay-close"
            className="p-1 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
            onClick={onClose}
          >
            <X className="w-4 h-4" aria-hidden />
          </button>
        </div>

        {/* Search */}
        <div className="px-4 py-2 border-b border-slate-100 dark:border-slate-800 flex-shrink-0">
          <input
            ref={searchRef}
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter shortcuts…"
            className={[
              'w-full bg-slate-50 dark:bg-slate-800 rounded-md px-3 py-1.5 text-sm outline-none',
              'text-slate-900 dark:text-slate-100',
              'placeholder:text-slate-400 dark:placeholder:text-slate-500',
              'border border-slate-200 dark:border-slate-700',
              'focus:border-sky-400 dark:focus:border-sky-600',
              'transition-colors',
            ].join(' ')}
            data-testid="shortcuts-overlay-search"
            aria-label="Filter shortcuts"
            autoComplete="off"
            spellCheck={false}
          />
        </div>

        {/* Categories */}
        <div className="overflow-y-auto flex-1 py-2" data-testid="shortcuts-overlay-list">
          {filtered.length === 0 && (
            <p className="px-4 py-6 text-center text-sm text-slate-400 dark:text-slate-500">
              No shortcuts match &ldquo;{search}&rdquo;
            </p>
          )}

          {filtered.map((cat) => {
            const isCollapsed = collapsed[cat.id] ?? false
            return (
              <div key={cat.id} data-testid={`shortcuts-category-${cat.id}`}>
                {/* Category heading — collapsible */}
                <button
                  type="button"
                  className={[
                    'w-full flex items-center gap-2 px-4 py-1.5 text-left',
                    'hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors',
                  ].join(' ')}
                  aria-expanded={!isCollapsed}
                  aria-controls={`shortcuts-section-${cat.id}`}
                  onClick={() => toggleCollapse(cat.id)}
                  data-testid={`shortcuts-category-toggle-${cat.id}`}
                >
                  {isCollapsed
                    ? <ChevronRight className="w-3 h-3 text-slate-400 flex-shrink-0" aria-hidden />
                    : <ChevronDown  className="w-3 h-3 text-slate-400 flex-shrink-0" aria-hidden />
                  }
                  <span className="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
                    {cat.label}
                  </span>
                  <span className="ml-auto text-[10px] text-slate-300 dark:text-slate-600">
                    {cat.shortcuts.length}
                  </span>
                </button>

                {/* Shortcut rows */}
                {!isCollapsed && (
                  <div
                    id={`shortcuts-section-${cat.id}`}
                    role="list"
                    aria-label={`${cat.label} shortcuts`}
                  >
                    {cat.shortcuts.map((shortcut, i) => (
                      <ShortcutRow
                        key={`${cat.id}-${i}`}
                        shortcut={shortcut}
                      />
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>

        {/* Footer */}
        <div className="flex items-center gap-3 px-4 py-2 border-t border-slate-100 dark:border-slate-800 flex-shrink-0 text-xs text-slate-400 dark:text-slate-500">
          <span><kbd className="font-mono">Esc</kbd> close</span>
          <span className="ml-auto">Press <kbd className="font-mono">?</kbd> anytime to reopen</span>
        </div>
      </div>
    </div>
  )
}

/* ── ShortcutRow ────────────────────────────────────────────────────────────── */

interface ShortcutRowProps {
  shortcut: Shortcut
}

function ShortcutRow({ shortcut }: ShortcutRowProps) {
  return (
    <div
      role="listitem"
      className="flex items-center gap-3 px-6 py-1.5 hover:bg-slate-50 dark:hover:bg-slate-800/40 transition-colors"
      data-testid="shortcut-row"
    >
      {/* Key(s) */}
      <div className="flex items-center gap-1 flex-shrink-0 min-w-[7rem]">
        {shortcut.keys.map((key, i) => (
          <span key={i} className="flex items-center gap-1">
            {i > 0 && (
              <span className="text-[10px] text-slate-300 dark:text-slate-600 mx-0.5">then</span>
            )}
            <kbd
              className={[
                'inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[11px] font-mono font-medium',
                'bg-slate-100 dark:bg-slate-800',
                'text-slate-700 dark:text-slate-300',
                'border border-slate-200 dark:border-slate-700',
                'shadow-[0_1px_0_rgba(0,0,0,0.1)] dark:shadow-[0_1px_0_rgba(0,0,0,0.4)]',
                'min-w-[1.5rem] text-center',
              ].join(' ')}
            >
              {key}
            </kbd>
          </span>
        ))}
      </div>

      {/* Description */}
      <span className="flex-1 text-sm text-slate-700 dark:text-slate-300 truncate">
        {shortcut.description}
      </span>

      {/* Scope badge */}
      <span
        className={[
          'flex-shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded',
          SCOPE_STYLES[shortcut.scope],
        ].join(' ')}
        data-testid="shortcut-scope"
      >
        {shortcut.scope}
      </span>
    </div>
  )
}
