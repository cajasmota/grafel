import { useState, useCallback, useEffect, useRef } from 'react'
import {
  X, ExternalLink, MapPin, Pin, PinOff, Copy, Check,
  ChevronDown, ChevronRight, Search, Network, Waypoints, Layers,
} from 'lucide-react'
import { KindBadge } from '@/components/shared/KindBadge'
import { RepoChip } from '@/components/shared/RepoChip'
import { SourceSnippet } from '@/components/shared/SourceSnippet'
import { NodeChip } from './NodeChip'
import { EdgeBadge } from './EdgeBadge'
import { ExportDiagramButton } from './ExportDiagramButton'
import type { EntityNeighborResponse, EntityKind, GraphEdge, GraphNode } from '@/types/api'

interface EntityInspectorProps {
  data: EntityNeighborResponse | undefined
  isLoading: boolean
  onClose: () => void
  onSelectEntity: (id: string) => void
  /** The active group name — required for the DSL export endpoint (#1318) */
  group?: string
  /** Deep-link to Flows surface for process_flow entities */
  onOpenInFlows?: (entityId: string) => void
  /** Deep-link to Paths surface for http_endpoint/handler entities */
  onOpenInPaths?: (entityId: string) => void
  /** Deep-link to Topology surface for message_topic entities */
  onOpenInTopology?: (entityId: string) => void
  /** Highlight the 1-hop subgraph of this entity in the canvas */
  onHighlightSubgraph?: (nodeIds: string[]) => void
  /**
   * When pinned the panel stays open after clicking another node (comparison
   * mode). The parent controls whether the pin is active; this callback
   * toggles the state.
   */
  pinned?: boolean
  onTogglePin?: () => void
}

// ── Surface routing helpers ───────────────────────────────────────────────────

const FLOW_KINDS: ReadonlySet<EntityKind> = new Set([
  'Process', 'Operation',
])

const PATH_KINDS: ReadonlySet<EntityKind> = new Set([
  'Endpoint', 'Route', 'Service',
])

const TOPIC_KINDS: ReadonlySet<EntityKind> = new Set([
  'MessageTopic', 'Queue', 'Event',
])

function surfaceAction(
  kind: EntityKind,
  entityId: string,
  props: Pick<EntityInspectorProps, 'onOpenInFlows' | 'onOpenInPaths' | 'onOpenInTopology'>,
): { label: string; icon: React.ReactNode; handler: () => void } | null {
  if (FLOW_KINDS.has(kind) && props.onOpenInFlows) {
    return {
      label: 'Open in Flows',
      icon: <Waypoints className="w-3.5 h-3.5" />,
      handler: () => props.onOpenInFlows!(entityId),
    }
  }
  if (PATH_KINDS.has(kind) && props.onOpenInPaths) {
    return {
      label: 'Open in Paths',
      icon: <ExternalLink className="w-3.5 h-3.5" />,
      handler: () => props.onOpenInPaths!(entityId),
    }
  }
  if (TOPIC_KINDS.has(kind) && props.onOpenInTopology) {
    return {
      label: 'Open in Topology',
      icon: <Layers className="w-3.5 h-3.5" />,
      handler: () => props.onOpenInTopology!(entityId),
    }
  }
  return null
}

// ── Storage helpers for section collapse persistence ──────────────────────────

const COLLAPSE_KEY = 'archigraph.inspector.collapsed'

function loadCollapsed(): Set<string> {
  try {
    const raw = localStorage.getItem(COLLAPSE_KEY)
    if (raw) return new Set(JSON.parse(raw) as string[])
  } catch { /* ignore */ }
  return new Set()
}

function saveCollapsed(s: Set<string>) {
  try {
    localStorage.setItem(COLLAPSE_KEY, JSON.stringify([...s]))
  } catch { /* ignore */ }
}

// ── Main component ────────────────────────────────────────────────────────────

/**
 * Slide-in panel (#1240): rich entity inspector for the graph surface.
 *
 * Sections:
 *  - Header: name + kind chip + repo chip + pin + copy-ID
 *  - Entity: qualified_name + kind badge + repo chip
 *  - Centrality: PageRank + betweenness + in/out degree
 *  - Community: resolved name for this entity's community_id
 *  - Source: file:line + SourceSnippet
 *  - Inbound / Outbound: edges grouped by kind with search filter
 *  - AI Summary: enrichment summary if present (via entity.properties)
 *  - Actions: surface deep-links + highlight subgraph + open in editor
 *
 * Keyboard: ArrowUp/ArrowDown cycle through neighbor entities when the panel
 * is focused and no text input is active.
 */
export function EntityInspector({
  data,
  isLoading,
  onClose,
  onSelectEntity,
  group,
  onOpenInFlows,
  onOpenInPaths,
  onOpenInTopology,
  onHighlightSubgraph,
  pinned = false,
  onTogglePin,
}: EntityInspectorProps) {
  // Section collapse state persisted to localStorage.
  const [collapsed, setCollapsed] = useState<Set<string>>(loadCollapsed)

  const toggleSection = useCallback((key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      saveCollapsed(next)
      return next
    })
  }, [])

  // Inbound/outbound search filter.
  const [edgeFilter, setEdgeFilter] = useState('')

  // Clear filter when entity changes.
  const prevIdRef = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (data?.entity.id !== prevIdRef.current) {
      setEdgeFilter('')
      prevIdRef.current = data?.entity.id
    }
  }, [data?.entity.id])

  // Copy ID button.
  const [copied, setCopied] = useState(false)
  const handleCopy = useCallback(() => {
    if (!data?.entity.id) return
    navigator.clipboard.writeText(data.entity.id).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }, [data?.entity.id])

  // Keyboard: Arrow keys cycle through neighbors.
  const neighborIds: string[] = data
    ? [...data.outbound.map((x) => x.node.id), ...data.inbound.map((x) => x.node.id)]
    : []
  const focusIdxRef = useRef(-1)

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return
      if (!neighborIds.length) return
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        focusIdxRef.current = (focusIdxRef.current + 1) % neighborIds.length
        onSelectEntity(neighborIds[focusIdxRef.current])
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        focusIdxRef.current =
          (focusIdxRef.current - 1 + neighborIds.length) % neighborIds.length
        onSelectEntity(neighborIds[focusIdxRef.current])
      }
    },
    [neighborIds, onSelectEntity],
  )

  // Highlight subgraph: entity + all 1-hop neighbors.
  const handleHighlight = useCallback(() => {
    if (!data || !onHighlightSubgraph) return
    const ids = [
      data.entity.id,
      ...data.outbound.map((x) => x.node.id),
      ...data.inbound.map((x) => x.node.id),
    ]
    onHighlightSubgraph(ids)
  }, [data, onHighlightSubgraph])

  // Surface deep-link button (Flows / Paths / Topology).
  const surfaceBtn = data
    ? surfaceAction(data.entity.kind, data.entity.id, {
        onOpenInFlows,
        onOpenInPaths,
        onOpenInTopology,
      })
    : null

  const aiSummary = data?.entity.properties?.['ai_summary']
    ? String(data.entity.properties['ai_summary'])
    : null

  return (
    <aside
      className="flex flex-col h-full bg-white dark:bg-slate-950 border-l border-slate-200 dark:border-slate-800 w-[360px] min-w-[280px] max-w-md overflow-hidden"
      aria-label="Entity inspector"
      role="complementary"
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      onKeyDown={handleKeyDown}
      data-testid="entity-inspector"
    >
      {/* ── Header ─────────────────────────────────────────────────────────── */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-900/60 sticky top-0 z-10 gap-2">
        <h2 className="text-sm font-semibold text-slate-800 dark:text-slate-200 truncate flex-1">
          {isLoading ? 'Loading…' : (data?.entity.label ?? 'Inspector')}
        </h2>

        {/* Copy entity ID */}
        {data && (
          <button
            type="button"
            onClick={handleCopy}
            aria-label="Copy entity ID"
            title="Copy entity ID"
            className="p-1 rounded text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
          >
            {copied
              ? <Check className="w-3.5 h-3.5 text-green-500" />
              : <Copy className="w-3.5 h-3.5" />}
          </button>
        )}

        {/* Pin/unpin */}
        {onTogglePin && (
          <button
            type="button"
            onClick={onTogglePin}
            aria-label={pinned ? 'Unpin panel' : 'Pin panel — keep open when selecting other nodes'}
            title={pinned ? 'Unpin panel' : 'Pin panel'}
            className={`p-1 rounded transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 ${
              pinned
                ? 'text-sky-500 hover:text-sky-400 bg-sky-50 dark:bg-sky-950'
                : 'text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800'
            }`}
          >
            {pinned ? <PinOff className="w-3.5 h-3.5" /> : <Pin className="w-3.5 h-3.5" />}
          </button>
        )}

        {/* Close */}
        <button
          type="button"
          onClick={onClose}
          aria-label="Close inspector"
          className="p-1 rounded text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* ── Loading ─────────────────────────────────────────────────────────── */}
      {isLoading && <InspectorSkeleton />}

      {/* ── Content ─────────────────────────────────────────────────────────── */}
      {!isLoading && data && (
        <div className="flex-1 flex flex-col gap-0 overflow-y-auto">

          {/* Entity metadata */}
          <CollapsibleSection
            title="Entity"
            sectionKey="entity"
            collapsed={collapsed}
            onToggle={toggleSection}
          >
            <div className="flex flex-wrap gap-2 items-center">
              <KindBadge kind={data.entity.kind} />
              <RepoChip repo={data.entity.repo} />
            </div>
            <p className="text-xs font-mono text-slate-400 dark:text-slate-500 mt-1 break-all">
              {data.entity.qualified_name}
            </p>
          </CollapsibleSection>

          {/* Centrality metrics */}
          {(data.entity.pagerank !== undefined ||
            data.betweenness !== undefined ||
            data.in_degree !== undefined ||
            data.out_degree !== undefined) && (
            <CollapsibleSection
              title="Centrality"
              sectionKey="centrality"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                {data.entity.pagerank !== undefined && (
                  <>
                    <dt className="text-slate-500 dark:text-slate-600">PageRank</dt>
                    <dd className="text-slate-700 dark:text-slate-300 font-mono tabular-nums">
                      {data.entity.pagerank.toFixed(5)}
                    </dd>
                  </>
                )}
                {data.betweenness !== undefined && (
                  <>
                    <dt className="text-slate-500 dark:text-slate-600">Betweenness</dt>
                    <dd className="text-slate-700 dark:text-slate-300 font-mono tabular-nums">
                      {data.betweenness.toFixed(4)}
                    </dd>
                  </>
                )}
                {data.in_degree !== undefined && (
                  <>
                    <dt className="text-slate-500 dark:text-slate-600">In-degree</dt>
                    <dd className="text-slate-700 dark:text-slate-300 font-mono tabular-nums">
                      {data.in_degree}
                    </dd>
                  </>
                )}
                {data.out_degree !== undefined && (
                  <>
                    <dt className="text-slate-500 dark:text-slate-600">Out-degree</dt>
                    <dd className="text-slate-700 dark:text-slate-300 font-mono tabular-nums">
                      {data.out_degree}
                    </dd>
                  </>
                )}
              </dl>
            </CollapsibleSection>
          )}

          {/* Community */}
          {(data.community_name || data.entity.community_id !== undefined) && (
            <CollapsibleSection
              title="Community"
              sectionKey="community"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              {data.community_name ? (
                <p className="text-xs text-slate-700 dark:text-slate-300">
                  {data.community_name}
                  {data.entity.community_id !== undefined && (
                    <span className="ml-1 text-slate-400 dark:text-slate-600">
                      (#{data.entity.community_id})
                    </span>
                  )}
                </p>
              ) : (
                <p className="text-xs text-slate-500 dark:text-slate-600">
                  Community #{data.entity.community_id}
                </p>
              )}
            </CollapsibleSection>
          )}

          {/* Source location */}
          {data.entity.source_file && (
            <CollapsibleSection
              title="Source"
              sectionKey="source"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              <SourceSnippet
                sourceFile={data.entity.source_file}
                startLine={data.entity.start_line}
                endLine={data.entity.end_line}
                language={data.entity.language}
              />
            </CollapsibleSection>
          )}

          {/* AI Summary */}
          {aiSummary && (
            <CollapsibleSection
              title="AI Summary"
              sectionKey="ai_summary"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              <p className="text-xs text-slate-600 dark:text-slate-400 leading-relaxed">
                {aiSummary}
              </p>
            </CollapsibleSection>
          )}

          {/* Edge filter input */}
          {(data.outbound.length > 0 || data.inbound.length > 0) && (
            <div className="px-4 pt-3 pb-1">
              <div className="relative">
                <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none" />
                <input
                  type="text"
                  placeholder="Filter edges…"
                  value={edgeFilter}
                  onChange={(e) => setEdgeFilter(e.target.value)}
                  className="w-full pl-7 pr-3 py-1 text-xs rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-700 dark:text-slate-300 placeholder-slate-400 focus:outline-none focus:ring-1 focus:ring-sky-400"
                  aria-label="Filter edges by name"
                />
              </div>
            </div>
          )}

          {/* Outbound edges */}
          {data.outbound.length > 0 && (
            <CollapsibleSection
              title={`Outbound (${data.outbound.length})`}
              sectionKey="outbound"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              <NeighborList
                items={data.outbound}
                filter={edgeFilter}
                onSelect={onSelectEntity}
              />
            </CollapsibleSection>
          )}

          {/* Inbound edges */}
          {data.inbound.length > 0 && (
            <CollapsibleSection
              title={`Inbound (${data.inbound.length})`}
              sectionKey="inbound"
              collapsed={collapsed}
              onToggle={toggleSection}
            >
              <NeighborList
                items={data.inbound}
                filter={edgeFilter}
                onSelect={onSelectEntity}
              />
            </CollapsibleSection>
          )}

          {/* Actions */}
          <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-800/60 flex flex-col gap-1.5">
            {/* Surface-specific deep-link */}
            {surfaceBtn && (
              <button
                type="button"
                onClick={surfaceBtn.handler}
                className="flex items-center gap-2 text-xs text-sky-500 hover:text-sky-400 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 rounded px-1"
              >
                {surfaceBtn.icon}
                {surfaceBtn.label}
              </button>
            )}

            {/* Fallback: Flows link when kind doesn't match a specific surface */}
            {!surfaceBtn && onOpenInFlows && (
              <button
                type="button"
                onClick={() => onOpenInFlows(data.entity.id)}
                className="flex items-center gap-2 text-xs text-sky-500 hover:text-sky-400 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 rounded px-1"
              >
                <ExternalLink className="w-3.5 h-3.5" />
                Open in Process Flows
              </button>
            )}

            {/* Highlight 1-hop subgraph */}
            {onHighlightSubgraph && (
              <button
                type="button"
                onClick={handleHighlight}
                className="flex items-center gap-2 text-xs text-violet-500 hover:text-violet-400 transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 rounded px-1"
              >
                <Network className="w-3.5 h-3.5" />
                Highlight subgraph
              </button>
            )}

            {/* Copy as diagram — DSL export (#1318) */}
            {group && (
              <ExportDiagramButton
                group={group}
                entityId={data.entity.id}
                depth={2}
              />
            )}

            {/* Open in editor */}
            {data.entity.source_file && (
              <a
                href={`vscode://file/${data.entity.source_file}:${data.entity.start_line}`}
                className="flex items-center gap-2 text-xs text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 transition-colors"
                onClick={(e) => e.stopPropagation()}
              >
                <MapPin className="w-3.5 h-3.5" />
                Open in editor
              </a>
            )}
          </div>
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

// ── CollapsibleSection ────────────────────────────────────────────────────────

function CollapsibleSection({
  title,
  sectionKey,
  collapsed,
  onToggle,
  children,
}: {
  title: string
  sectionKey: string
  collapsed: Set<string>
  onToggle: (key: string) => void
  children: React.ReactNode
}) {
  const isCollapsed = collapsed.has(sectionKey)
  return (
    <div className="border-b border-slate-200 dark:border-slate-800/60">
      <button
        type="button"
        className="w-full flex items-center justify-between px-4 py-2 text-left focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-sky-400 hover:bg-slate-50 dark:hover:bg-slate-900/40 transition-colors"
        onClick={() => onToggle(sectionKey)}
        aria-expanded={!isCollapsed}
      >
        <span className="text-[10px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-600">
          {title}
        </span>
        {isCollapsed
          ? <ChevronRight className="w-3 h-3 text-slate-400 flex-shrink-0" />
          : <ChevronDown className="w-3 h-3 text-slate-400 flex-shrink-0" />}
      </button>
      {!isCollapsed && (
        <div className="px-4 pb-3">
          {children}
        </div>
      )}
    </div>
  )
}

// ── NeighborList ──────────────────────────────────────────────────────────────

function NeighborList({
  items,
  filter,
  onSelect,
}: {
  items: Array<{ edge: GraphEdge; node: GraphNode }>
  filter: string
  onSelect: (id: string) => void
}) {
  const lf = filter.toLowerCase()
  const filtered = lf
    ? items.filter((x) => x.node.label.toLowerCase().includes(lf))
    : items

  // Group by edge kind.
  const grouped = new Map<string, Array<{ edge: GraphEdge; node: GraphNode }>>()
  for (const item of filtered) {
    const k = item.edge.kind
    if (!grouped.has(k)) grouped.set(k, [])
    grouped.get(k)!.push(item)
  }

  if (filtered.length === 0) {
    return (
      <p className="text-xs text-slate-400 dark:text-slate-600 italic">
        No matches
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {[...grouped.entries()].map(([kind, entries]) => (
        <div key={kind}>
          <div className="mb-1">
            <EdgeBadge kind={kind as Parameters<typeof EdgeBadge>[0]['kind']} />
          </div>
          <ul className="flex flex-col gap-0.5 pl-1" aria-label={`${kind} edges`}>
            {entries.slice(0, 20).map(({ edge, node }) => (
              <li key={edge.id}>
                <NodeChip
                  kind={node.kind}
                  label={node.label}
                  repo={node.repo}
                  onClick={() => onSelect(node.id)}
                />
              </li>
            ))}
            {entries.length > 20 && (
              <li className="text-[10px] text-slate-500 dark:text-slate-600 pl-2">
                +{entries.length - 20} more
              </li>
            )}
          </ul>
        </div>
      ))}
    </div>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

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
