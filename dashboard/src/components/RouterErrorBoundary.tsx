import { useRouteError, isRouteErrorResponse } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, Bug } from 'lucide-react'

export function RouterErrorBoundary() {
  const error = useRouteError()

  // 404 or other HTTP error response
  if (isRouteErrorResponse(error)) {
    const status = error.status
    const attemptedPath = error.statusText || window.location.pathname

    if (status === 404) {
      return (
        <div className="flex flex-col h-screen bg-white dark:bg-slate-950 text-slate-800 dark:text-slate-200">
          <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-200 dark:border-slate-800 flex-shrink-0 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm z-20">
            <span className="text-sm font-bold tracking-tight text-sky-400">archigraph</span>
          </header>

          <main className="flex-1 flex items-center justify-center overflow-hidden">
            <div className="flex flex-col items-center gap-8 text-center max-w-lg px-4">
              {/* Icon */}
              <div className="rounded-full bg-slate-200 dark:bg-slate-800 p-4">
                <AlertTriangle className="w-8 h-8 text-amber-500" aria-hidden />
              </div>

              {/* Title */}
              <div className="space-y-2">
                <h1 className="text-3xl font-bold text-slate-800 dark:text-slate-200">Page Not Found</h1>
                <p className="text-sm text-slate-400 dark:text-slate-500">
                  We couldn't find <code className="font-mono text-slate-400 dark:text-slate-400 bg-slate-200 dark:bg-slate-800 px-2 py-1 rounded">{attemptedPath}</code>
                </p>
              </div>

              {/* Navigation surfaces */}
              <div className="space-y-4 w-full">
                <p className="text-sm text-slate-400 dark:text-slate-400">Navigate to one of these surfaces:</p>
                <nav className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                  <a
                    href="/graph/fixture-a"
                    className="px-4 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors text-sm font-medium border border-slate-300 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600"
                  >
                    Graph
                  </a>
                  <a
                    href="/flows/fixture-a"
                    className="px-4 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors text-sm font-medium border border-slate-300 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600"
                  >
                    Flows
                  </a>
                  <a
                    href="/topology/fixture-a"
                    className="px-4 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors text-sm font-medium border border-slate-300 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600"
                  >
                    Topology
                  </a>
                  <a
                    href="/paths/fixture-a"
                    className="px-4 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors text-sm font-medium border border-slate-300 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600"
                  >
                    Paths
                  </a>
                  <a
                    href="/docs/fixture-a"
                    className="px-4 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors text-sm font-medium border border-slate-300 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600"
                  >
                    Docs
                  </a>
                </nav>
              </div>

              {/* Primary CTA */}
              <a
                href="/graph/fixture-a"
                className="px-6 py-3 rounded-lg bg-sky-600 hover:bg-sky-500 text-white transition-colors font-semibold flex items-center gap-2"
              >
                <ArrowLeft className="w-4 h-4" aria-hidden />
                Back to Graph
              </a>
            </div>
          </main>
        </div>
      )
    }

    // Other HTTP errors (5xx, etc.)
    return (
      <div className="flex flex-col h-screen bg-white dark:bg-slate-950 text-slate-800 dark:text-slate-200">
        <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-200 dark:border-slate-800 flex-shrink-0 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm z-20">
          <span className="text-sm font-bold tracking-tight text-sky-400">archigraph</span>
        </header>

        <main className="flex-1 flex items-center justify-center overflow-hidden">
          <div className="flex flex-col items-center gap-6 text-center max-w-lg px-4">
            {/* Icon */}
            <div className="rounded-full bg-slate-200 dark:bg-slate-800 p-4">
              <AlertTriangle className="w-8 h-8 text-red-500" aria-hidden />
            </div>

            {/* Title */}
            <div className="space-y-2">
              <h1 className="text-3xl font-bold text-slate-800 dark:text-slate-200">Error {status}</h1>
              <p className="text-sm text-slate-400 dark:text-slate-500">{error.statusText || 'An error occurred'}</p>
            </div>

            {/* Error details */}
            {error.data && (
              <div className="w-full text-left">
                <p className="text-xs text-slate-400 dark:text-slate-400 bg-slate-100/50 dark:bg-slate-900/50 border border-slate-200 dark:border-slate-800 rounded p-3 font-mono break-words">
                  {error.data}
                </p>
              </div>
            )}

            {/* Actions */}
            <div className="flex flex-col gap-3 w-full">
              <button
                onClick={() => window.location.reload()}
                className="px-6 py-3 rounded-lg bg-slate-200 dark:bg-slate-800 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors font-semibold"
              >
                Reload
              </button>
              <a
                href="https://github.com/cajasmota/archigraph/issues/new"
                target="_blank"
                rel="noopener noreferrer"
                className="px-6 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors font-semibold flex items-center justify-center gap-2"
              >
                <Bug className="w-4 h-4" aria-hidden />
                Report Issue
              </a>
            </div>
          </div>
        </main>
      </div>
    )
  }

  // Unhandled runtime error
  return (
    <div className="flex flex-col h-screen bg-white dark:bg-slate-950 text-slate-800 dark:text-slate-200">
      <header className="flex items-center gap-4 px-4 h-12 border-b border-slate-200 dark:border-slate-800 flex-shrink-0 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm z-20">
        <span className="text-sm font-bold tracking-tight text-sky-400">archigraph</span>
      </header>

      <main className="flex-1 flex items-center justify-center overflow-hidden">
        <div className="flex flex-col items-center gap-6 text-center max-w-lg px-4">
          {/* Icon */}
          <div className="rounded-full bg-slate-200 dark:bg-slate-800 p-4">
            <AlertTriangle className="w-8 h-8 text-red-500" aria-hidden />
          </div>

          {/* Title */}
          <div className="space-y-2">
            <h1 className="text-3xl font-bold text-slate-800 dark:text-slate-200">Something Went Wrong</h1>
            <p className="text-sm text-slate-400 dark:text-slate-500">An unexpected error occurred while rendering this page.</p>
          </div>

          {/* Error message */}
          {error instanceof Error && (
            <div className="w-full text-left">
              <p className="text-xs text-slate-400 dark:text-slate-400 bg-slate-100/50 dark:bg-slate-900/50 border border-slate-200 dark:border-slate-800 rounded p-3 font-mono break-words">
                {error.message}
              </p>
            </div>
          )}

          {/* Actions */}
          <div className="flex flex-col gap-3 w-full">
            <button
              onClick={() => window.location.reload()}
              className="px-6 py-3 rounded-lg bg-slate-200 dark:bg-slate-800 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors font-semibold"
            >
              Reload
            </button>
            <a
              href="https://github.com/cajasmota/archigraph/issues/new"
              target="_blank"
              rel="noopener noreferrer"
              className="px-6 py-3 rounded-lg bg-slate-200/50 dark:bg-slate-800/50 hover:bg-slate-300 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300 hover:text-slate-800 dark:hover:text-slate-200 transition-colors font-semibold flex items-center justify-center gap-2"
            >
              <Bug className="w-4 h-4" aria-hidden />
              Report Issue
            </a>
          </div>
        </div>
      </main>
    </div>
  )
}
