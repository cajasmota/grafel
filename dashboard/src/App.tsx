import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { ThemeProvider } from '@/context/ThemeContext'
import { AppLayout } from '@/routes/_layout'
import { IndexRoute } from '@/routes/index'
import { GraphRoute } from '@/routes/graph'
import { FlowsRoute } from '@/routes/flows'
import { TopologyRoute } from '@/routes/topology'
import { PathsRoute } from '@/routes/paths'
import { PathsDetailRoute } from '@/routes/paths.detail'
import { DocsRoute } from '@/routes/docs'
import { PendingRoute } from '@/routes/pending'
import { DiagnosticsRoute } from '@/routes/diagnostics'
import { PatternsRoute } from '@/routes/patterns'
import { SystemRoute } from '@/routes/system'
import { UpdateRoute } from '@/routes/update'
import { SettingsRoute } from '@/routes/settings'
import { RouterErrorBoundary } from '@/components/RouterErrorBoundary'
import { EmptyState } from '@/components/shared/EmptyState'
import { Globe } from 'lucide-react'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 2,
      staleTime: 30 * 1000,
      gcTime: 5 * 60 * 1000,
    },
  },
})

const router = createBrowserRouter([
  {
    path: '/',
    element: <AppLayout />,
    errorElement: <RouterErrorBoundary />,
    children: [
      { index: true, element: <IndexRoute /> },

      // Surface 1 — Graph
      { path: 'graph', element: <Navigate to="/graph/fixture-a" replace /> },
      { path: 'graph/:group', element: <GraphRoute /> },

      // Surface 2 — Flows
      { path: 'flows', element: <Navigate to="/flows/fixture-a" replace /> },
      { path: 'flows/:group', element: <FlowsRoute /> },

      // Surface 3 — Topology
      { path: 'topology', element: <Navigate to="/topology/fixture-a" replace /> },
      { path: 'topology/:group', element: <TopologyRoute /> },

      // Surface 4 — API Explorer (with nested detail route)
      { path: 'paths', element: <Navigate to="/paths/fixture-a" replace /> },
      {
        path: 'paths/:group',
        element: <PathsRoute />,
        children: [
          {
            index: true,
            element: (
              <div className="h-full flex items-center justify-center text-slate-500 dark:text-slate-600">
                <EmptyState
                  icon={Globe}
                  title="Select a path"
                  message="Click any path in the list to view its handlers, response shapes, and dependencies."
                />
              </div>
            ),
          },
          { path: ':pathHash', element: <PathsDetailRoute /> },
        ],
      },

      // Surface 5 — Docs
      { path: 'docs', element: <Navigate to="/docs/fixture-a" replace /> },
      { path: 'docs/:group', element: <DocsRoute /> },
      { path: 'docs/:group/*', element: <DocsRoute /> },

      // Surface 6 — Pending queue (repairs + enrichments, #987)
      { path: 'pending', element: <Navigate to="/pending/fixture-a" replace /> },
      { path: 'pending/:group', element: <PendingRoute /> },

      // Surface 7 — Diagnostics (#1187)
      { path: 'diagnostics', element: <DiagnosticsRoute /> },

      // Surface 8 — Patterns (#1189)
      { path: 'patterns', element: <Navigate to="/patterns/fixture-a" replace /> },
      { path: 'patterns/:group', element: <PatternsRoute /> },

      // Surface 9 — System / daemon control panel (#1195)
      { path: 'system', element: <SystemRoute /> },

      // Surface 10 — Update / Version management (#1199)
      { path: 'update', element: <UpdateRoute /> },

      // Surface 11 — Settings (#1206)
      { path: 'settings', element: <SettingsRoute /> },
    ],
  },
])

export function App() {
  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
        {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
      </QueryClientProvider>
    </ThemeProvider>
  )
}
