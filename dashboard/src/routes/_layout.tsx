import { Link, NavLink, Outlet, useParams } from 'react-router-dom'
import { useEffect } from 'react'
import {
  GitBranch, Network, Workflow, Radio, Globe, BookOpen,
  Moon, Sun,
} from 'lucide-react'
import { useRegistry } from '@/hooks/shared/useRegistry'
import { useThemeContext } from '@/context/ThemeContext'
import { GroupSelector } from '@/components/layout/GroupSelector'
import { VersionPopover } from '@/components/layout/VersionPopover'

const GROUP_DEFAULT = 'fixture-a'

export function AppLayout() {
  const { group = GROUP_DEFAULT } = useParams()
  const { data: registry } = useRegistry()
  const groups = registry?.groups ?? []

  // Keyboard shortcut: g h → go home
  useEffect(() => {
    let lastG = false
    const handler = (e: KeyboardEvent) => {
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
  }, [])

  return (
    <div className="flex flex-col h-screen bg-slate-950 text-slate-200">
      {/* ── Top nav ──────────────────────────────────────────────────────────── */}
      <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-800 flex-shrink-0 bg-slate-950/90 backdrop-blur-sm z-20">
        {/* Logo */}
        <Link to="/" className="flex items-center gap-2 font-bold text-sm tracking-tight text-sky-400 hover:text-sky-300">
          <GitBranch className="w-5 h-5" aria-hidden />
          archigraph
        </Link>

        {/* Surface nav — 5 chips (Graph / Flows / Topology / Paths / Docs) */}
        <nav className="flex items-center gap-0.5 ml-2 sm:ml-4 sm:gap-1 flex-shrink-0" aria-label="Surface navigation">
          <NavItem to={`/graph/${group}`} icon={<Network className="w-4 h-4" />} label="Graph" />
          <NavItem to={`/flows/${group}`} icon={<Workflow className="w-4 h-4" />} label="Flows" />
          <NavItem to={`/topology/${group}`} icon={<Radio className="w-4 h-4" />} label="Topology" />
          <NavItem to={`/paths/${group}`} icon={<Globe className="w-4 h-4" />} label="Paths" />
          <NavItem to={`/docs/${group}`} icon={<BookOpen className="w-4 h-4" />} label="Docs" />
        </nav>

        <div className="ml-auto flex items-center gap-2">
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
    </div>
  )
}

interface NavItemProps {
  to: string
  icon: React.ReactNode
  label: string
}

function NavItem({ to, icon, label }: NavItemProps) {
  return (
    <NavLink
      to={to}
      aria-label={label}
      className={({ isActive }) =>
        [
          'flex items-center gap-1.5 px-2 py-1.5 rounded text-sm transition-colors',
          'sm:px-3',
          isActive
            ? 'bg-slate-800 text-slate-200'
            : 'text-slate-500 hover:bg-slate-800/60 hover:text-slate-300',
        ].join(' ')
      }
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </NavLink>
  )
}

function ThemeToggle() {
  const { isDark, toggle } = useThemeContext()

  return (
    <button
      type="button"
      aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
      className="p-1.5 rounded text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors"
      onClick={toggle}
    >
      {isDark ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
    </button>
  )
}
