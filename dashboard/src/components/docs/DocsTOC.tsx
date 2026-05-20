import { cn } from '@/lib/utils'
import type { TocHeading } from '@/types/docs'

interface DocsTOCProps {
  headings: TocHeading[]
  activeId: string | null
}

/**
 * Right-rail table of contents.
 * Renders h2 and h3 headings with scroll-spy active highlight.
 * Clicking a heading smooth-scrolls to it and updates the URL hash.
 */
export function DocsTOC({ headings, activeId }: DocsTOCProps) {
  if (headings.length === 0) return null

  const handleClick = (id: string) => {
    const el = document.getElementById(id)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      window.history.pushState(null, '', `#${id}`)
    }
  }

  return (
    <nav
      aria-label="On this page"
      className="space-y-0.5"
    >
      <p className="text-xs font-semibold text-slate-400 dark:text-slate-400 uppercase tracking-wider mb-3">
        On this page
      </p>
      {headings.map(({ id, text, depth }) => (
        <button
          key={id}
          type="button"
          onClick={() => handleClick(id)}
          className={cn(
            'block w-full text-left text-sm py-0.5 transition-colors leading-snug',
            depth === 3 && 'pl-4',
            activeId === id
              ? 'text-sky-400 font-medium'
              : 'text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300',
          )}
        >
          {text}
        </button>
      ))}
    </nav>
  )
}
