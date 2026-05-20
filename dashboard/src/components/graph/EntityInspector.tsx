import { X, ExternalLink, MapPin } from 'lucide-react'
import { KindBadge } from '@/components/shared/KindBadge'
import { RepoChip } from '@/components/shared/RepoChip'
import { SourceSnippet } from '@/components/shared/SourceSnippet'
import { NodeChip } from './NodeChip'
import { EdgeBadge } from './EdgeBadge'
import type { EntityNeighborResponse, GraphEdge, GraphNode } from '@/types/api'

interface EntityInspectorProps {
  data: EntityNeighborResponse | undefined
  isLoading: boolean
  onClose: () => void
  onSelectEntity: (id: string) => void
  /** Deep-link to Surface 2 (flows) for this entity */
  onOpenInFlows?: (entityId: string) => void
}

/**
 * Slide-in panel: entity metadata, outbound edges grouped by kind,
 * source snippet, and "open in flows" link.
 *
 * Provides a list-view alternative to the 3D canvas for a11y.
 */
export function EntityInspector({
  data,
  isLoading,
  onClose,
  onSelectEntity,
  onOpenInFlows,
}: EntityInspectorProps) {
  return (
    <aside
      className="flex flex-col h-full bg-white dark:bg-slate-950 border-l border-slate-200 dark:border-slate-800 w-[340px] min-w-[280px] max-w-md overflow-y-auto"
      aria-label="Entity inspector"
      role="complementary"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-900/60 sticky top-0 z-10">
        <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200 truncate">
          {isLoading ? 'Loading…' : (data?.entity.label ?? 'Inspector')}
        </h2>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close inspector"
          className="p-1 rounded text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {isLoading && <InspectorSkeleton />}

      {!isLoading && data && (
        <div className="flex-1 flex flex-col gap-0 overflow-y-auto">
          {/* Metadata */}
          <Section title="Entity">
            <div className="flex flex-wrap gap-2 items-center">
              <KindBadge kind={data.entity.kind} />
              <RepoChip repo={data.entity.repo} />
            </div>
            <p className="text-xs font-mono text-slate-400 dark:text-slate-400 mt-1 break-all">
              {data.entity.qualified_name}
            </p>
            {data.entity.pagerank !== undefined && (
              <p className="text-xs text-slate-500 dark:text-slate-600 mt-0.5">
                PageRank: {data.entity.pagerank.toFixed(4)}
              </p>
            )}
          </Section>

          {/* Source location */}
          {data.entity.source_file && (
            <Section title="Source">
              <SourceSnippet
                sourceFile={data.entity.source_file}
                startLine={data.entity.start_line}
                endLine={data.entity.end_line}
                language={data.entity.language}
              />
            </Section>
          )}

          {/* Outbound edges grouped by kind */}
          {data.outbound.length > 0 && (
            <Section title={`Outbound (${data.outbound.length})`}>
              <NeighborList
                items={data.outbound}
                onSelect={onSelectEntity}
              />
            </Section>
          )}

          {/* Inbound edges */}
          {data.inbound.length > 0 && (
            <Section title={`Inbound (${data.inbound.length})`}>
              <NeighborList
                items={data.inbound}
                onSelect={onSelectEntity}
              />
            </Section>
          )}

          {/* Actions */}
          <Section title="">
            <div className="flex flex-col gap-1">
              {onOpenInFlows && (
                <button
                  type="button"
                  onClick={() => onOpenInFlows(data.entity.id)}
                  className="flex items-center gap-2 text-xs text-sky-400 hover:text-sky-300 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 rounded px-1"
                >
                  <ExternalLink className="w-3.5 h-3.5" />
                  Open in Process Flows
                </button>
              )}
              <a
                href={`vscode://file/${data.entity.source_file}:${data.entity.start_line}`}
                className="flex items-center gap-2 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 transition-colors"
                onClick={(e) => e.stopPropagation()}
              >
                <MapPin className="w-3.5 h-3.5" />
                Open in editor
              </a>
            </div>
          </Section>
        </div>
      )}

      {!isLoading && !data && (
        <div className="flex-1 flex items-center justify-center p-6 text-center text-sm text-slate-500 dark:text-slate-600">
          Select a node to inspect it.
        </div>
      )}
    </aside>
  )
}

// ── Subcomponents ─────────────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-800/60">
      {title && (
        <h3 className="text-[10px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-600 mb-2">
          {title}
        </h3>
      )}
      {children}
    </div>
  )
}

function NeighborList({
  items,
  onSelect,
}: {
  items: Array<{ edge: GraphEdge; node: GraphNode }>
  onSelect: (id: string) => void
}) {
  // Group by edge kind
  const grouped = new Map<string, Array<{ edge: GraphEdge; node: GraphNode }>>()
  for (const item of items) {
    const k = item.edge.kind
    if (!grouped.has(k)) grouped.set(k, [])
    grouped.get(k)!.push(item)
  }

  return (
    <div className="flex flex-col gap-2">
      {[...grouped.entries()].map(([kind, entries]) => (
        <div key={kind}>
          <div className="mb-1">
            <EdgeBadge kind={kind as Parameters<typeof EdgeBadge>[0]['kind']} />
          </div>
          <ul className="flex flex-col gap-0.5 pl-1" aria-label={`${kind} edges`}>
            {entries.slice(0, 15).map(({ edge, node }) => (
              <li key={edge.id}>
                <NodeChip
                  kind={node.kind}
                  label={node.label}
                  repo={node.repo}
                  onClick={() => onSelect(node.id)}
                />
              </li>
            ))}
            {entries.length > 15 && (
              <li className="text-[10px] text-slate-500 dark:text-slate-600 pl-2">
                +{entries.length - 15} more
              </li>
            )}
          </ul>
        </div>
      ))}
    </div>
  )
}

function InspectorSkeleton() {
  return (
    <div className="p-4 flex flex-col gap-3" role="status" aria-label="Loading entity…">
      <div className="h-5 w-3/4 rounded animate-pulse bg-slate-200 dark:bg-slate-800" />
      <div className="h-4 w-1/2 rounded animate-pulse bg-slate-200 dark:bg-slate-800" />
      <div className="h-20 w-full rounded animate-pulse bg-slate-200 dark:bg-slate-800" />
      <div className="h-4 w-full rounded animate-pulse bg-slate-200 dark:bg-slate-800" />
      <div className="h-4 w-5/6 rounded animate-pulse bg-slate-200 dark:bg-slate-800" />
      <span className="sr-only">Loading…</span>
    </div>
  )
}
