import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import type { DocBreadcrumb } from '@/types/docs'

interface DocsBreadcrumbsProps {
  group: string
  crumbs: DocBreadcrumb[]
}

/**
 * Path-derived crumb trail derived from doc content response.
 * Last crumb is the current page — rendered without a link.
 */
export function DocsBreadcrumbs({ group, crumbs }: DocsBreadcrumbsProps) {
  return (
    <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-sm text-slate-400 dark:text-slate-500 mb-6">
      {crumbs.map((crumb, i) => (
        <span key={i} className="flex items-center gap-1 min-w-0">
          {i > 0 && <ChevronRight className="w-3.5 h-3.5 flex-shrink-0 text-slate-500 dark:text-slate-600" aria-hidden />}
          {crumb.path ? (
            <Link
              to={`/docs/${group}/${crumb.path}`}
              className="hover:text-slate-700 dark:hover:text-slate-300 transition-colors truncate"
            >
              {crumb.label}
            </Link>
          ) : (
            <span className="text-slate-700 dark:text-slate-300 truncate" aria-current="page">
              {crumb.label}
            </span>
          )}
        </span>
      ))}
    </nav>
  )
}
