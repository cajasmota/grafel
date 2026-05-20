import { BookOpen, RefreshCw } from 'lucide-react'

interface DocsEmptyStateProps {
  group: string
  onRetry?: () => void
}

/**
 * Empty state shown when a group has no generated documentation yet.
 *
 * Renders:
 *  - Heading + explanation paragraph
 *  - Numbered step list with the /generate-docs skill command
 *  - Code block for the command
 *  - "Try Again" button to re-fetch
 */
export function DocsEmptyState({ group, onRetry }: DocsEmptyStateProps) {
  return (
    <div
      className="flex flex-col items-center justify-center h-full py-16 px-6 text-center"
      role="status"
      aria-live="polite"
    >
      <div className="rounded-full bg-slate-100 dark:bg-slate-800 p-4 mb-4">
        <BookOpen className="w-8 h-8 text-slate-500 dark:text-slate-500" aria-hidden />
      </div>

      <h2 className="text-lg font-semibold text-slate-800 dark:text-slate-200 mb-2">
        No documentation generated yet
      </h2>

      <p className="max-w-md text-sm text-slate-600 dark:text-slate-400 mb-6">
        Archigraph can generate per-entity documentation summarising what each
        function, class, and component does — drawing context from the
        surrounding code graph. Once generated, docs appear here as a
        searchable, cross-linked portal.
      </p>

      <ol className="text-left max-w-md w-full space-y-3 mb-6 list-none">
        <li className="flex gap-3 text-sm text-slate-700 dark:text-slate-300">
          <span className="flex-shrink-0 w-5 h-5 rounded-full bg-indigo-600 text-white text-xs flex items-center justify-center font-semibold">
            1
          </span>
          <span>
            Open Claude Code in a repo registered under{' '}
            <code className="px-1 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-indigo-600 dark:text-indigo-300 text-xs font-mono">
              {group}
            </code>{' '}
            and run the skill:
            <br />
            <code className="mt-1 inline-block px-2 py-1 rounded bg-slate-100 dark:bg-slate-800 text-emerald-700 dark:text-emerald-300 text-xs font-mono border border-slate-200 dark:border-slate-700">
              /generate-docs
            </code>
          </span>
        </li>

        <li className="flex gap-3 text-sm text-slate-700 dark:text-slate-300">
          <span className="flex-shrink-0 w-5 h-5 rounded-full bg-indigo-600 text-white text-xs flex items-center justify-center font-semibold">
            2
          </span>
          <span>
            Wait for the pipeline to complete — typically 5–20 minutes
            depending on codebase size.
          </span>
        </li>

        <li className="flex gap-3 text-sm text-slate-700 dark:text-slate-300">
          <span className="flex-shrink-0 w-5 h-5 rounded-full bg-indigo-600 text-white text-xs flex items-center justify-center font-semibold">
            3
          </span>
          <span>Refresh this page — the docs tree will appear in the sidebar.</span>
        </li>
      </ol>

      {onRetry && (
        <button
          onClick={onRetry}
          className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700 text-sm text-slate-700 dark:text-slate-200 border border-slate-200 dark:border-slate-600 transition-colors"
          type="button"
        >
          <RefreshCw className="w-4 h-4" aria-hidden />
          Try Again
        </button>
      )}
    </div>
  )
}
