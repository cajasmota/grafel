import { useRef, useState, useCallback, useEffect } from 'react'
import { TopicNode } from './TopicNode'
import { ProducerSpoke } from './ProducerSpoke'
import { ConsumerSpoke } from './ConsumerSpoke'
import { TransformEdge } from './TransformEdge'
import type { TopologyLayout } from '@/hooks/topology/useTopologyLayout'
import type { TopologyResponse } from '@/types/api'

interface TopologyMapProps {
  layout: TopologyLayout
  data: TopologyResponse
  selectedId: string | null
  onSelectTopic: (id: string | null) => void
}

const CANVAS_W = 1200
const CANVAS_H = 700
const SPOKE_RADIUS = 80 // pixels from node center to spoke endpoint

/**
 * Main 2D force-layout topology graph.
 * Renders as SVG with pan+zoom via CSS transforms on a wrapper div.
 * Keyboard nav: arrow keys move between nodes, Enter selects, Esc deselects.
 * Spokes are rendered as SVG lines radiating from each hub node at equal angles.
 */
export function TopologyMap({ layout, data, selectedId, onSelectTopic }: TopologyMapProps) {
  const svgRef = useRef<SVGSVGElement>(null)
  const [focusedId, setFocusedId] = useState<string | null>(null)
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 })
  const dragRef = useRef<{ startX: number; startY: number; tx: number; ty: number } | null>(null)

  const nodeIds = layout.nodes.map((n) => n.id)
  const nodeMap = new Map(layout.nodes.map((n) => [n.id, n]))

  // Build entity → position map from layout for spoke endpoints
  // Producer/consumer endpoints are placed radially around their topic hub
  function getSpokeEndpoints(
    topicX: number,
    topicY: number,
    entityIds: string[],
    side: 'in' | 'out',
  ): Array<{ id: string; x: number; y: number }> {
    const total = entityIds.length
    if (total === 0) return []
    return entityIds.map((id, i) => {
      const baseAngle = side === 'in' ? Math.PI : 0
      const spread = Math.min(Math.PI * 0.8, (total - 1) * 0.4)
      const angle = total === 1 ? baseAngle : baseAngle - spread / 2 + (spread / (total - 1)) * i
      return {
        id,
        x: topicX + Math.cos(angle) * SPOKE_RADIUS,
        y: topicY + Math.sin(angle) * SPOKE_RADIUS,
      }
    })
  }

  // Keyboard navigation across nodes
  const handleNodeKeyDown = useCallback(
    (e: React.KeyboardEvent, id: string) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        onSelectTopic(id === selectedId ? null : id)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onSelectTopic(null)
        setFocusedId(null)
      } else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
        e.preventDefault()
        const idx = nodeIds.indexOf(id)
        const next = nodeIds[(idx + 1) % nodeIds.length]
        setFocusedId(next)
        document.getElementById(`topo-node-${next}`)?.focus()
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        e.preventDefault()
        const idx = nodeIds.indexOf(id)
        const prev = nodeIds[(idx - 1 + nodeIds.length) % nodeIds.length]
        setFocusedId(prev)
        document.getElementById(`topo-node-${prev}`)?.focus()
      }
    },
    [nodeIds, selectedId, onSelectTopic],
  )

  // Pan handling
  const handleMouseDown = useCallback((e: React.MouseEvent<SVGSVGElement>) => {
    if ((e.target as Element).closest('[role="button"]')) return
    dragRef.current = { startX: e.clientX, startY: e.clientY, tx: transform.x, ty: transform.y }
  }, [transform])

  const handleMouseMove = useCallback((e: React.MouseEvent<SVGSVGElement>) => {
    if (!dragRef.current) return
    const dx = e.clientX - dragRef.current.startX
    const dy = e.clientY - dragRef.current.startY
    setTransform((prev) => ({ ...prev, x: dragRef.current!.tx + dx, y: dragRef.current!.ty + dy }))
  }, [])

  const handleMouseUp = useCallback(() => { dragRef.current = null }, [])

  // Zoom via wheel
  const handleWheel = useCallback((e: WheelEvent) => {
    e.preventDefault()
    setTransform((prev) => {
      const delta = e.deltaY > 0 ? 0.9 : 1.1
      return { ...prev, scale: Math.min(3, Math.max(0.3, prev.scale * delta)) }
    })
  }, [])

  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    svg.addEventListener('wheel', handleWheel, { passive: false })
    return () => svg.removeEventListener('wheel', handleWheel)
  }, [handleWheel])

  // Esc deselects when canvas has focus
  const handleSvgKeyDown = useCallback((e: React.KeyboardEvent<SVGSVGElement>) => {
    if (e.key === 'Escape') {
      onSelectTopic(null)
      setFocusedId(null)
    }
  }, [onSelectTopic])

  // Build producer/consumer entity positions alongside their topics
  const allProducerStubs = data.producers ?? {}
  const allConsumerStubs = data.consumers ?? {}

  if (layout.nodes.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-slate-600 text-sm">
        No topics match the current filters
      </div>
    )
  }

  return (
    <svg
      ref={svgRef}
      role="application"
      aria-label="Broker topology map — use arrow keys to navigate topics, Enter to select"
      className="flex-1 w-full h-full bg-slate-950 cursor-grab active:cursor-grabbing select-none"
      viewBox={`0 0 ${CANVAS_W} ${CANVAS_H}`}
      onMouseDown={handleMouseDown}
      onMouseMove={handleMouseMove}
      onMouseUp={handleMouseUp}
      onMouseLeave={handleMouseUp}
      onKeyDown={handleSvgKeyDown}
      tabIndex={-1}
    >
      <g transform={`translate(${CANVAS_W / 2 + transform.x},${CANVAS_H / 2 + transform.y}) scale(${transform.scale})`}>
        {/* Render transform edges first (lowest z-order) */}
        {layout.edges
          .filter((e) => e.kind === 'transform')
          .map((edge) => {
            const from = nodeMap.get(edge.source)
            const to = nodeMap.get(edge.target)
            if (!from || !to) return null
            return (
              <TransformEdge
                key={`transform-${edge.source}-${edge.target}`}
                fromId={edge.source}
                toId={edge.target}
                fromX={from.x}
                fromY={from.y}
                toX={to.x}
                toY={to.y}
                isHighlighted={selectedId === edge.source || selectedId === edge.target}
              />
            )
          })}

        {/* Render producer/consumer spokes per topic node */}
        {layout.nodes.map((node) => {
          const isHighlighted = !selectedId || selectedId === node.id

          // Collect producer_ids and consumer_ids for this node
          const topicData =
            (data.topics ?? []).find((t) => t.id === node.id) ??
            (data.queues ?? []).find((q) => q.id === node.id) ??
            (data.nats_subjects ?? []).find((n) => n.id === node.id) ??
            (data.functions ?? []).find((f) => f.id === node.id)

          if (!topicData) return null

          // FunctionNode uses invoker_ids/handler_ids; all other node types use
          // producer_ids/consumer_ids. Fall back to empty arrays when absent.
          const producerIds: string[] =
            ('producer_ids' in topicData ? topicData.producer_ids : null) ??
            ('invoker_ids' in topicData ? (topicData as { invoker_ids: string[] }).invoker_ids : null) ??
            []
          const consumerIds: string[] =
            ('consumer_ids' in topicData ? topicData.consumer_ids : null) ??
            ('handler_ids' in topicData ? (topicData as { handler_ids: string[] }).handler_ids : null) ??
            []

          const producerEndpoints = getSpokeEndpoints(node.x, node.y, producerIds, 'in')
          const consumerEndpoints = getSpokeEndpoints(node.x, node.y, consumerIds, 'out')

          return (
            <g key={`spokes-${node.id}`}>
              {producerEndpoints.map(({ id: pid, x: px, y: py }) => {
                const stub = allProducerStubs[pid]
                if (!stub) return null
                return (
                  <ProducerSpoke
                    key={`prod-${node.id}-${pid}`}
                    producerId={pid}
                    producerLabel={stub.label}
                    producerRepo={stub.repo}
                    px={px}
                    py={py}
                    tx={node.x}
                    ty={node.y}
                    isHighlighted={isHighlighted}
                  />
                )
              })}
              {consumerEndpoints.map(({ id: cid, x: cx, y: cy }) => {
                const stub = allConsumerStubs[cid]
                if (!stub) return null
                return (
                  <ConsumerSpoke
                    key={`cons-${node.id}-${cid}`}
                    consumerId={cid}
                    consumerLabel={stub.label}
                    consumerRepo={stub.repo}
                    tx={node.x}
                    ty={node.y}
                    cx={cx}
                    cy={cy}
                    isHighlighted={isHighlighted}
                  />
                )
              })}
            </g>
          )
        })}

        {/* Render topic hub nodes (top z-order) */}
        {layout.nodes.map((node) => (
          <TopicNode
            key={node.id}
            id={node.id}
            label={node.label}
            protocol={node.protocol}
            spokeCount={node.spokeCount}
            isSelected={selectedId === node.id}
            isFocused={focusedId === node.id}
            x={node.x}
            y={node.y}
            onClick={(id) => {
              onSelectTopic(id === selectedId ? null : id)
              setFocusedId(id)
            }}
            onKeyDown={handleNodeKeyDown}
          />
        ))}
      </g>
    </svg>
  )
}
