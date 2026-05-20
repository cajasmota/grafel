import { Link } from 'react-router-dom'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import type { DocNavLink } from '@/types/docs'

interface DocsPrevNextProps {
  group: string
  prev?: DocNavLink
  next?: DocNavLink
}

/**
 * Bottom-of-page navigation: previous and next doc links
 * derived from the flat tree order in the doc content response.
 */
export function DocsPrevNext({ group, prev, next }: DocsPrevNextProps) {
  if (!prev && !next) return null

  return (
    <nav
      aria-label="Pagination navigation"
      className="flex items-stretch justify-between gap-4 mt-12 pt-6 border-t border-slate-200 dark:border-slate-800"
    >
      {prev ? (
        <Link
          to={`/docs/${group}/${prev.path}`}
          className="flex items-center gap-3 flex-1 max-w-[calc(50%-8px)] p-4 rounded-lg border border-slate-200 dark:border-slate-800 hover:border-slate-400 dark:hover:border-slate-600 hover:bg-slate-200 dark:hover:bg-slate-900 transition-colors group"
        >
          <ChevronLeft className="w-5 h-5 flex-shrink-0 text-slate-400 dark:text-slate-500 group-hover:text-sky-400 transition-colors" aria-hidden />
          <div className="min-w-0">
            <div className="text-xs text-slate-400 dark:text-slate-500 mb-0.5">Previous</div>
            <div className="text-sm font-medium text-slate-700 dark:text-slate-300 group-hover:text-slate-100 truncate">
              {prev.label}
            </div>
          </div>
        </Link>
      ) : (
        <div className="flex-1" />
      )}

      {next ? (
        <Link
          to={`/docs/${group}/${next.path}`}
          className="flex items-center gap-3 flex-1 max-w-[calc(50%-8px)] p-4 rounded-lg border border-slate-200 dark:border-slate-800 hover:border-slate-400 dark:hover:border-slate-600 hover:bg-slate-200 dark:hover:bg-slate-900 transition-colors group text-right justify-end"
        >
          <div className="min-w-0">
            <div className="text-xs text-slate-400 dark:text-slate-500 mb-0.5">Next</div>
            <div className="text-sm font-medium text-slate-700 dark:text-slate-300 group-hover:text-slate-100 truncate">
              {next.label}
            </div>
          </div>
          <ChevronRight className="w-5 h-5 flex-shrink-0 text-slate-400 dark:text-slate-500 group-hover:text-sky-400 transition-colors" aria-hidden />
        </Link>
      ) : (
        <div className="flex-1" />
      )}
    </nav>
  )
}
