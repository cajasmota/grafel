import { useParams, useLocation } from 'react-router-dom'
import { useDocTree } from '@/hooks/docs/useDocTree'
import { useDocContent } from '@/hooks/docs/useDocContent'
import { DocsSidebar } from './DocsSidebar'
import { DocsContent } from './DocsContent'
import { DocsTopSearch } from './DocsTopSearch'
import { PathTreeSkeleton, CardSkeleton } from '@/components/shared/LoadingState'
import { EmptyState } from '@/components/shared/EmptyState'
import { ErrorBoundary } from '@/components/shared/ErrorBoundary'
import { BookOpen, FileX } from 'lucide-react'

interface DocsPageProps {
  group: string
  /** The doc path segments after /docs/:group/, e.g. "acme-web/modules/auth/overview" */
  docPath?: string
}

/**
 * Top-level layout for Surface 5 — Docs Portal.
 *
 * Layout: [skip-to-content] | [sidebar 240px] | [content + right-rail TOC]
 *
 * Sidebar is always visible at ≥md. Content area is max-w-[720px] for readability.
 *
 * A11y:
 * - Skip-to-content link at top (#main-content)
 * - Sidebar has role="navigation" + aria-label
 * - Active page has aria-current="page"
 * - Keyboard: / focuses search, Esc closes dropdown
 */
export function DocsPage({ group, docPath }: DocsPageProps) {
  const { data: treeData, isLoading: treeLoading, error: treeError } = useDocTree(group)
  const { data: content, isLoading: contentLoading, error: contentError } = useDocContent(group, docPath)

  return (
    <div className="flex flex-col h-full bg-slate-950">
      {/* Search bar at top */}
      <div className="flex items-center gap-4 px-4 py-3 border-b border-slate-800 bg-slate-950/50 flex-shrink-0">
        <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Docs Search</span>
        <DocsTopSearch group={group} />
      </div>

      {/* Main content area */}
      <div className="flex h-full overflow-hidden">
        {/* Skip-to-content */}
        <a href="#main-content" className="skip-link focus:translate-y-0">
          Skip to content
        </a>

        {/* Sidebar */}
        <aside
          className="hidden md:flex flex-col w-60 flex-shrink-0 border-r border-slate-800 bg-slate-950/80 overflow-y-auto"
          aria-label="Documentation navigation"
        >
          {/* Sidebar header */}
          <div className="flex items-center px-4 py-3 border-b border-slate-800">
            <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Navigation</span>
          </div>

          {treeLoading && (
            <div className="p-4">
              <PathTreeSkeleton />
            </div>
          )}
          {treeError && (
            <div className="p-4 text-xs text-rose-400">Failed to load navigation</div>
          )}
          {treeData && (
            <DocsSidebar
              group={group}
              tree={treeData.tree}
              currentPath={docPath}
            />
          )}
        </aside>

        {/* Main content */}
        <main className="flex-1 overflow-y-auto min-w-0">
        <ErrorBoundary
          fallback={
            <EmptyState
              icon={FileX}
              title="Error rendering doc"
              message="This document could not be rendered. Check the console for details."
            />
          }
        >
          {!docPath && (
            <div className="flex flex-col items-center justify-center h-full">
              <EmptyState
                icon={BookOpen}
                title="Select a document"
                message="Choose a document from the sidebar to start reading."
              />
            </div>
          )}

          {docPath && contentLoading && (
            <div className="p-8 space-y-4 max-w-3xl">
              <CardSkeleton />
              <CardSkeleton />
            </div>
          )}

          {docPath && contentError && (
            <div className="flex flex-col items-center justify-center h-full">
              <EmptyState
                icon={FileX}
                title="Document not found"
                message={`Could not load "${docPath}". It may not exist yet.`}
              />
            </div>
          )}

          {docPath && content && (
            <DocsContent group={group} content={content} />
          )}
        </ErrorBoundary>
        </main>
      </div>
    </div>
  )
}
