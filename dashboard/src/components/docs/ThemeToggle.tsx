import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/hooks/docs/useTheme'

/**
 * Light/dark switcher. Persists to localStorage + applies .dark to <html>.
 * Keyboard: Enter/Space to toggle (native button).
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { isDark, toggle } = useTheme()

  return (
    <button
      type="button"
      aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
      className={[
        'p-1.5 rounded text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors',
        className,
      ]
        .filter(Boolean)
        .join(' ')}
      onClick={toggle}
    >
      {isDark ? <Sun className="w-4 h-4" aria-hidden /> : <Moon className="w-4 h-4" aria-hidden />}
    </button>
  )
}
