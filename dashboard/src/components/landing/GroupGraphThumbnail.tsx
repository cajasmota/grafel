/**
 * GroupGraphThumbnail — 80px-tall inline SVG graph preview for landing cards (#983).
 *
 * Renders a static positional snapshot (top-200 nodes by degree, pre-computed
 * radial layout, no Cosmograph/WebGL) fetched lazily from
 * GET /api/graph/{group}/layout-snapshot.
 *
 * Features:
 *  - Skeleton loading state (pulse animation matches card chrome)
 *  - Empty-state placeholder when group has no entities or snapshot is absent
 *  - Community-based fill colour palette (12 distinct hues, cycling)
 *  - Dot radius scales with node degree (hub nodes appear larger)
 *  - Click on the thumbnail (or a specific dot) navigates to /graph/<group>
 *  - Hover: subtle border-glow + slight scale-up via CSS
 */

import { useRef, useState } from 'react'
import { useGraphThumbnail } from '@/hooks/shared/useGraphThumbnail'
import type { ThumbnailNode } from '@/types/api'

// ─────────────────────────────────────────────────────────────────────────────
// Colour palette — 12 community hues, cycling by (communityId % 12).
// Uses slate-family backgrounds so thumbnails feel cohesive with the card.
// ─────────────────────────────────────────────────────────────────────────────

const COMMUNITY_PALETTE = [
  '#38bdf8', // sky-400
  '#818cf8', // indigo-400
  '#34d399', // emerald-400
  '#fb923c', // orange-400
  '#f472b6', // pink-400
  '#a78bfa', // violet-400
  '#facc15', // yellow-400
  '#2dd4bf', // teal-400
  '#60a5fa', // blue-400
  '#f87171', // red-400
  '#4ade80', // green-400
  '#e879f9', // fuchsia-400
] as const

function communityColor(communityId?: number, repo?: string): string {
  if (communityId !== undefined) {
    return COMMUNITY_PALETTE[communityId % COMMUNITY_PALETTE.length]
  }
  // Fallback: hash repo slug to a consistent palette index.
  if (repo) {
    let h = 0
    for (let i = 0; i < repo.length; i++) h = (h * 31 + repo.charCodeAt(i)) >>> 0
    return COMMUNITY_PALETTE[h % COMMUNITY_PALETTE.length]
  }
  return '#475569' // slate-600 — unclustered fallback
}

// ─────────────────────────────────────────────────────────────────────────────
// Dot radius: logarithmic scale, min 1.5px, max 5px (viewport-relative).
// ─────────────────────────────────────────────────────────────────────────────

function dotRadius(degree: number, maxDegree: number): number {
  if (maxDegree <= 0) return 1.5
  const ratio = degree / maxDegree
  return 1.5 + Math.sqrt(ratio) * 3.5
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-components
// ─────────────────────────────────────────────────────────────────────────────

function ThumbnailSkeleton() {
  return (
    <div
      role="status"
      aria-label="Loading graph preview…"
      className={[
        'w-full h-[80px] rounded-t-xl',
        'bg-slate-800 animate-pulse',
      ].join(' ')}
    >
      <span className="sr-only">Loading graph preview…</span>
    </div>
  )
}

function ThumbnailEmpty() {
  return (
    <div
      aria-label="No graph preview available"
      className={[
        'w-full h-[80px] rounded-t-xl',
        'flex items-center justify-center',
        'bg-slate-900/40 border-b border-slate-800/50',
      ].join(' ')}
    >
      <span className="text-[10px] text-slate-600 font-mono uppercase tracking-widest select-none">
        not indexed
      </span>
    </div>
  )
}

interface ThumbnailSVGProps {
  nodes: ThumbnailNode[]
  onNodeClick?: (nodeId: string) => void
}

function ThumbnailSVG({ nodes, onNodeClick }: ThumbnailSVGProps) {
  const svgRef = useRef<SVGSVGElement>(null)

  // SVG viewport: fixed logical size, scales via viewBox.
  const W = 400
  const H = 80

  const maxDegree = Math.max(1, ...nodes.map((n) => n.d))

  return (
    <svg
      ref={svgRef}
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="xMidYMid meet"
      aria-label="Graph topology preview"
      className="w-full h-full"
      style={{ display: 'block' }}
    >
      {/* Background matches card chrome */}
      <rect width={W} height={H} fill="#0f172a" rx="12" ry="12" />

      {nodes.map((node) => {
        const x = node.x * W
        const y = node.y * H
        const r = dotRadius(node.d, maxDegree)
        const fill = communityColor(node.c, node.repo)

        return (
          <circle
            key={node.id}
            cx={x}
            cy={y}
            r={r}
            fill={fill}
            opacity={0.85}
            style={{ cursor: onNodeClick ? 'pointer' : 'default' }}
            onClick={onNodeClick ? (e) => { e.stopPropagation(); onNodeClick(node.id) } : undefined}
          >
            <title>{node.id}</title>
          </circle>
        )
      })}
    </svg>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Public component
// ─────────────────────────────────────────────────────────────────────────────

interface GroupGraphThumbnailProps {
  /** Group ID (used as the URL segment for the snapshot API call) */
  group: string
  /**
   * Called when the user clicks on a specific node dot.
   * Receives the node's prefixed ID (repo:hash).
   * If not provided, the whole thumbnail acts as a single click target (handled by the card button).
   */
  onNodeClick?: (nodeId: string) => void
  /** Whether to fetch the thumbnail at all (set false when card is out of viewport). */
  enabled?: boolean
}

export function GroupGraphThumbnail({ group, onNodeClick, enabled = true }: GroupGraphThumbnailProps) {
  const { data, isLoading, isError } = useGraphThumbnail(group, enabled)
  const [hovered, setHovered] = useState(false)

  const nodes = data?.nodes ?? []
  const isEmpty = nodes.length === 0

  // Wrapper div: clamps to 80px, clips overflow, adds hover glow.
  const wrapperCls = [
    'w-full h-[80px] overflow-hidden rounded-t-xl shrink-0',
    'transition-all duration-200',
    hovered && !isEmpty && !isLoading
      ? 'ring-1 ring-sky-500/40 scale-[1.01]'
      : '',
  ].join(' ')

  if (isLoading) return <ThumbnailSkeleton />
  if (isError || isEmpty) return <ThumbnailEmpty />

  return (
    <div
      className={wrapperCls}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <ThumbnailSVG nodes={nodes} onNodeClick={onNodeClick} />
    </div>
  )
}
