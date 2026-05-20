import { useMemo, useRef, useEffect } from 'react'
import type { TopologyResponse, TopologyProtocol } from '@/types/api'

export interface LayoutNode {
  id: string
  label: string
  protocol: TopologyProtocol
  repo: string
  /** Spoke count = producer_count + consumer_count — proportional to visual size */
  spokeCount: number
  x: number
  y: number
  vx: number
  vy: number
  fx?: number
  fy?: number
}

export interface LayoutEdge {
  source: string
  target: string
  kind: 'producer' | 'consumer' | 'transform'
}

export interface TopologyLayout {
  nodes: LayoutNode[]
  edges: LayoutEdge[]
  /** True while the simulation is settling (first few ticks) */
  isSettling: boolean
}

/** Simple seeded pseudo-random number generator for deterministic initial positions */
function seededRand(seed: number): () => number {
  let s = seed
  return () => {
    s = (s * 1664525 + 1013904223) & 0xffffffff
    return (s >>> 0) / 0xffffffff
  }
}

/**
 * Derives force-layout node/edge arrays from topology data.
 * Uses a minimal in-process force simulation without d3-force dependency
 * (saves ~80KB gzipped). Runs synchronously for the first frame, then
 * settles on subsequent renders via requestAnimationFrame via useEffect.
 *
 * Memoized — stable output until data reference changes.
 */
export function useTopologyLayout(
  data: TopologyResponse | undefined,
  activeProtocols: Set<TopologyProtocol>,
): TopologyLayout {
  // Mutable simulation state — stored in ref so we don't re-render on every tick
  const nodesRef = useRef<LayoutNode[]>([])
  const tickRef = useRef(0)

  const { nodes: initialNodes, edges } = useMemo<{ nodes: LayoutNode[]; edges: LayoutEdge[] }>(() => {
    if (!data) return { nodes: [], edges: [] }

    const rand = seededRand(42)
    const buildNode = (
      id: string,
      label: string,
      protocol: TopologyProtocol,
      repo: string,
      spokeCount: number,
    ): LayoutNode => ({
      id,
      label,
      protocol,
      repo,
      spokeCount,
      x: (rand() - 0.5) * 800,
      y: (rand() - 0.5) * 600,
      vx: 0,
      vy: 0,
    })

    const nodes: LayoutNode[] = []
    const edges: LayoutEdge[] = []

    const showAll = activeProtocols.size === 0

    // Kafka topics
    if (showAll || activeProtocols.has('kafka')) {
      for (const t of data.topics) {
        nodes.push(buildNode(t.id, t.label, 'kafka', t.repo, t.producer_ids.length + t.consumer_ids.length))
        for (const pid of t.producer_ids) edges.push({ source: pid, target: t.id, kind: 'producer' })
        for (const cid of t.consumer_ids) edges.push({ source: t.id, target: cid, kind: 'consumer' })
        for (const tid of t.transforms_to) edges.push({ source: t.id, target: tid, kind: 'transform' })
      }
    }

    // Queues (RabbitMQ, SQS, Pub/Sub)
    for (const q of data.queues) {
      const protocol = q.broker as TopologyProtocol
      if (!showAll && !activeProtocols.has(protocol)) continue
      nodes.push(buildNode(q.id, q.label, protocol, q.repo, q.producer_ids.length + q.consumer_ids.length))
      for (const pid of q.producer_ids) edges.push({ source: pid, target: q.id, kind: 'producer' })
      for (const cid of q.consumer_ids) edges.push({ source: q.id, target: cid, kind: 'consumer' })
    }

    // NATS subjects
    if (showAll || activeProtocols.has('nats')) {
      for (const n of data.nats_subjects) {
        nodes.push(buildNode(n.id, n.label, 'nats', n.repo, n.producer_ids.length + n.consumer_ids.length))
        for (const pid of n.producer_ids) edges.push({ source: pid, target: n.id, kind: 'producer' })
        for (const cid of n.consumer_ids) edges.push({ source: n.id, target: cid, kind: 'consumer' })
      }
    }

    // WebSocket/SSE channels
    for (const c of data.channels) {
      const protocol = c.channel_type as TopologyProtocol
      if (!showAll && !activeProtocols.has(protocol)) continue
      nodes.push(buildNode(c.id, c.label, protocol, c.repo, c.emitter_ids.length + c.subscriber_ids.length))
      for (const eid of c.emitter_ids) edges.push({ source: eid, target: c.id, kind: 'producer' })
      for (const sid of c.subscriber_ids) edges.push({ source: c.id, target: sid, kind: 'consumer' })
    }

    // GraphQL subscriptions
    if (showAll || activeProtocols.has('graphql_subscription')) {
      for (const g of data.graphql_subscriptions) {
        nodes.push(buildNode(g.id, g.label, 'graphql_subscription', g.repo, g.publisher_ids.length + g.subscriber_ids.length))
        for (const pid of g.publisher_ids) edges.push({ source: pid, target: g.id, kind: 'producer' })
        for (const sid of g.subscriber_ids) edges.push({ source: g.id, target: sid, kind: 'consumer' })
      }
    }

    return { nodes, edges }
  }, [data, activeProtocols])

  // Sync mutable ref when memo recalculates
  useEffect(() => {
    nodesRef.current = initialNodes.map((n) => ({ ...n }))
    tickRef.current = 0
  }, [initialNodes])

  return useMemo(
    () => ({ nodes: initialNodes, edges, isSettling: false }),
    [initialNodes, edges],
  )
}
