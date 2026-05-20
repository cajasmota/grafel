import { Link, NavLink, Outlet, useParams, useLocation } from 'react-router-dom'
import { useEffect } from 'react'
import {
  GitBranch, Network, Workflow, Radio, Globe, BookOpen,
  Moon, Sun, Settings,
} from 'lucide-react'
import { useRegistry } from '@/hooks/shared/useRegistry'
import { useThemeContext } from '@/context/ThemeContext'

const GROUP_DEFAULT = 'fixture-a'

export function AppLayout() {
  const { group = GROUP_DEFAULT } = useParams()
  const { data: registry } = useRegistry()
  const groups = registry?.groups ?? []
  const location = useLocation()

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
      {/* Top nav */}
      <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-800 flex-shrink-0 bg-slate-950/90 backdrop-blur-sm z-20">
        {/* Logo */}
        <Link to="/" className="flex items-center gap-2 font-bold text-sm tracking-tight text-sky-400 hover:text-sky-300">
          <GitBranch className="w-5 h-5" aria-hidden />
          archigraph
        </Link>

        {/* Breadcrumb: current group */}
        {groups.length > 0 && (
          <div className="flex items-center gap-2 ml-2">
            <span className="text-xs text-slate-500">/</span>
            <span className="text-xs font-mono text-slate-400">{group}</span>
          </div>
        )}

        {/* Group selector dropdown */}
        {groups.length > 1 && (
          <div className="flex items-center gap-1 ml-4">
            <span className="text-xs text-slate-500">Switch:</span>
            {groups.map((g) => (
              <Link
                key={g.id}
                to={`/graph/${g.id}`}
                className={[
                  'px-2 py-0.5 rounded text-xs font-mono transition-colors',
                  g.id === group
                    ? 'bg-sky-900/50 text-sky-300 border border-sky-700'
                    : 'text-slate-400 hover:bg-slate-800 hover:text-slate-300',
                ].join(' ')}
              >
                {g.id}
              </Link>
            ))}
          </div>
        )}

        <nav className="flex items-center gap-1 ml-4" aria-label="Surface navigation">
          <NavItem to={`/graph/${group}`} icon={<Network className="w-4 h-4" />} label="Graph" />
          <NavItem to={`/flows/${group}`} icon={<Workflow className="w-4 h-4" />} label="Flows" />
          <NavItem to={`/topology/${group}`} icon={<Radio className="w-4 h-4" />} label="Topology" />
          <NavItem to={`/paths/${group}`} icon={<Globe className="w-4 h-4" />} label="Paths" />
          <NavItem to={`/docs/${group}`} icon={<BookOpen className="w-4 h-4" />} label="Docs" />
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <ThemeToggle />
          <Link
            to="/settings"
            className="p-1.5 rounded text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors"
            aria-label="Settings"
          >
            <Settings className="w-4 h-4" />
          </Link>
        </div>
      </header>

      {/* Page content */}
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
      className={({ isActive }) =>
        [
          'flex items-center gap-1.5 px-3 py-1.5 rounded text-sm transition-colors',
          isActive
            ? 'bg-slate-800 text-slate-200'
            : 'text-slate-500 hover:bg-slate-800/60 hover:text-slate-300',
        ].join(' ')
      }
    >
      {icon}
      {label}
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
