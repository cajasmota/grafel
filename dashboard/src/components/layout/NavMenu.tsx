/**
 * NavMenu — grouped dropdown menus for the top navigation bar.
 *
 * Two menus:
 *   Explore: Graph, Flows, Topology, Paths, Docs, Pending
 *   Operate: Diagnostics, Quality, Patterns, System, Update, Settings, MCP Activity, Help
 *
 * Built on @radix-ui/react-dropdown-menu for keyboard nav, a11y,
 * and proper focus management out of the box.
 *
 * Hover prefetch (#1257):
 *   Hovering over Graph / Flows / Topology menu items for HOVER_DELAY_MS fires
 *   a react-query prefetchQuery so data is cached before the click lands.
 */

import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { NavLink, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import {
  Network, Workflow, Radio, Globe, BookOpen, Clock,
  Stethoscope, Sparkles, Server, RefreshCw, ChevronDown,
  BarChart2, Settings, Activity, ClipboardList, Zap, HelpCircle,
  ShieldAlert,
} from 'lucide-react'
import {
  prefetchSurface,
  HOVER_DELAY_MS,
  type PrefetchSurface,
} from '@/lib/prefetcher'

/* ── Types ──────────────────────────────────────────────────────────────────── */

interface NavEntry {
  label: string
  to: string
  icon: React.ReactNode
}

interface NavMenuProps {
  /** Display label shown on the trigger button */
  label: string
  /** data-testid for the trigger button */
  testId: string
  /** Items in this menu group */
  items: NavEntry[]
  /** Whether any item in the group is currently active */
  isGroupActive: boolean
  /** Current group slug — used for hover-prefetch target URLs (#1257) */
  group?: string
}

/* ── Helpers ────────────────────────────────────────────────────────────────── */

/** Build the href for group-scoped surfaces. */
export function exploreItems(group: string): NavEntry[] {
  return [
    { label: 'Graph',    to: `/graph/${group}`,    icon: <Network    className="w-4 h-4" /> },
    { label: 'Flows',    to: `/flows/${group}`,    icon: <Workflow   className="w-4 h-4" /> },
    { label: 'Topology', to: `/topology/${group}`, icon: <Radio      className="w-4 h-4" /> },
    { label: 'Paths',    to: `/paths/${group}`,    icon: <Globe      className="w-4 h-4" /> },
    { label: 'Docs',     to: `/docs/${group}`,     icon: <BookOpen   className="w-4 h-4" /> },
    { label: 'Pending',  to: `/pending/${group}`,  icon: <Clock      className="w-4 h-4" /> },
  ]
}

export function operateItems(group: string): NavEntry[] {
  return [
    { label: 'Diagnostics',   to: '/diagnostics',          icon: <Stethoscope  className="w-4 h-4" /> },
    { label: 'Security',      to: `/security/${group}`,    icon: <ShieldAlert  className="w-4 h-4" /> },
    { label: 'Quality',       to: '/quality',              icon: <BarChart2    className="w-4 h-4" /> },
    { label: 'Patterns',      to: `/patterns/${group}`,    icon: <Sparkles     className="w-4 h-4" /> },
    { label: 'System',        to: '/system',            icon: <Server      className="w-4 h-4" /> },
    { label: 'Update',        to: '/update',            icon: <RefreshCw   className="w-4 h-4" /> },
    { label: 'MCP Activity',  to: '/mcp-activity',      icon: <Activity       className="w-4 h-4" /> },
    { label: 'Audit Log',     to: '/audit-log',         icon: <ClipboardList  className="w-4 h-4" /> },
    { label: 'MCP Setup',     to: '/mcp-setup',         icon: <Zap         className="w-4 h-4" /> },
    { label: 'Settings',      to: '/settings',          icon: <Settings    className="w-4 h-4" /> },
    { label: 'Help & About',  to: '/help',              icon: <HelpCircle  className="w-4 h-4" /> },
  ]
}

/* ── NavMenu component ──────────────────────────────────────────────────────── */

/** Map label → PrefetchSurface for the 3 data-heavy explore surfaces. */
const PREFETCH_SURFACE_MAP: Record<string, PrefetchSurface> = {
  Graph: 'graph',
  Flows: 'flows',
  Topology: 'topology',
}

export function NavMenu({ label, testId, items, isGroupActive, group }: NavMenuProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  // Track per-item hover timers so we can cancel on mouse-leave
  const hoverTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  const triggerCls = [
    'flex items-center gap-1 px-2.5 py-1.5 rounded text-sm font-medium transition-colors',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400',
    isGroupActive
      ? 'bg-slate-100 dark:bg-slate-800 text-slate-900 dark:text-slate-100'
      : 'text-slate-500 hover:bg-slate-100/70 dark:hover:bg-slate-800/60 hover:text-slate-700 dark:hover:text-slate-300',
  ].join(' ')

  function handleItemMouseEnter(itemLabel: string) {
    const surface = PREFETCH_SURFACE_MAP[itemLabel]
    if (!surface || !group) return
    hoverTimers.current[itemLabel] = setTimeout(() => {
      prefetchSurface(queryClient, surface, group).catch(() => {
        // prefetch failures are non-fatal — surface will fetch on demand
      })
    }, HOVER_DELAY_MS)
  }

  function handleItemMouseLeave(itemLabel: string) {
    const t = hoverTimers.current[itemLabel]
    if (t !== undefined) {
      clearTimeout(t)
      delete hoverTimers.current[itemLabel]
    }
  }

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className={triggerCls}
          data-testid={testId}
          aria-haspopup="menu"
        >
          <span className="hidden sm:inline">{label}</span>
          {/* Mobile: show abbreviated label */}
          <span className="sm:hidden text-xs">{label.slice(0, 3)}</span>
          <ChevronDown className="w-3 h-3 opacity-60" aria-hidden />
          {isGroupActive && (
            <span
              className="ml-0.5 w-1.5 h-1.5 rounded-full bg-sky-400 flex-shrink-0"
              aria-label="active"
            />
          )}
        </button>
      </DropdownMenu.Trigger>

      <DropdownMenu.Portal>
        <DropdownMenu.Content
          className={[
            'z-50 min-w-[180px] rounded-lg border border-slate-200 dark:border-slate-700',
            'bg-white dark:bg-slate-900 shadow-xl py-1',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
            'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          ].join(' ')}
          sideOffset={6}
          align="start"
          data-testid={`${testId}-content`}
        >
          <p className="px-3 py-1.5 text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wider">
            {label}
          </p>
          <DropdownMenu.Separator className="h-px bg-slate-100 dark:bg-slate-800 my-1" />

          {items.map((item) => (
            <DropdownMenu.Item
              key={item.to}
              className="outline-none"
              onSelect={() => navigate(item.to)}
              onMouseEnter={() => handleItemMouseEnter(item.label)}
              onMouseLeave={() => handleItemMouseLeave(item.label)}
            >
              <MenuNavLink to={item.to} icon={item.icon} label={item.label} />
            </DropdownMenu.Item>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

/* ── MenuNavLink ────────────────────────────────────────────────────────────── */

interface MenuNavLinkProps {
  to: string
  icon: React.ReactNode
  label: string
}

function MenuNavLink({ to, icon, label }: MenuNavLinkProps) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        [
          'flex items-center gap-2.5 px-3 py-1.5 text-sm transition-colors w-full rounded-md mx-1',
          // total width accounts for the mx-1 inset
          'w-[calc(100%-8px)]',
          isActive
            ? 'bg-sky-50 dark:bg-sky-950/40 text-sky-700 dark:text-sky-300 font-medium'
            : 'text-slate-600 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/60 hover:text-slate-900 dark:hover:text-slate-200',
        ].join(' ')
      }
    >
      {icon}
      {label}
    </NavLink>
  )
}

/* ── useIsGroupActive ────────────────────────────────────────────────────────── */

/**
 * Returns true if the current location matches any of the given path prefixes.
 * Used to show the active indicator on the parent trigger button.
 */
export function useIsGroupActive(prefixes: string[]): boolean {
  // We can't call hooks conditionally, so we match against a sentinel
  // and just check pathname manually.
  if (typeof window === 'undefined') return false
  const path = window.location.pathname
  return prefixes.some((p) => path.startsWith(p))
}
