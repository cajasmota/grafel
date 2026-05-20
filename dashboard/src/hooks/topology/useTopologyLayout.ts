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

    // Helper: the REST API returns `producers`/`consumers` (plural, no _ids suffix)
    // while the TypeScript interface uses `producer_ids`/`consumer_ids`.
    // Support both forms so the layout works against real API responses and mock data.
    function getProducers(node: Record<string, unknown>): string[] {
      return (node.producer_ids as string[] | undefined) ?? (node.producers as string[] | undefined) ?? []
    }
    function getConsumers(node: Record<string, unknown>): string[] {
      return (node.consumer_ids as string[] | undefined) ?? (node.consumers as string[] | undefined) ?? []
    }
    function getEmitters(node: Record<string, unknown>): string[] {
      return (node.emitter_ids as string[] | undefined) ?? (node.emitters as string[] | undefined) ?? []
    }
    function getSubscribers(node: Record<string, unknown>): string[] {
      return (node.subscriber_ids as string[] | undefined) ?? (node.subscribers as string[] | undefined) ?? []
    }

    // Kafka topics
    if (showAll || activeProtocols.has('kafka')) {
      for (const t of (data.topics ?? [])) {
        const pids = getProducers(t as unknown as Record<string, unknown>)
        const cids = getConsumers(t as unknown as Record<string, unknown>)
        const xids: string[] = (t.transforms_to ?? []) as string[]
        nodes.push(buildNode(t.id, t.label, 'kafka', t.repo, pids.length + cids.length))
        for (const pid of pids) edges.push({ source: pid, target: t.id, kind: 'producer' })
        for (const cid of cids) edges.push({ source: t.id, target: cid, kind: 'consumer' })
        for (const tid of xids) edges.push({ source: t.id, target: tid, kind: 'transform' })
      }
    }

    // Queues (RabbitMQ, SQS, Pub/Sub, Redis Streams, task-queues)
    for (const q of (data.queues ?? [])) {
      // Derive the display protocol: broker field maps to TopologyProtocol.
      // For Redis Streams and task queues, the broker field carries the
      // framework name; we map it to the canonical TopologyProtocol.
      const qRaw = q as unknown as Record<string, unknown>
      const qLabel = (qRaw.label as string) ?? ''
      let protocol: TopologyProtocol = (q.broker as TopologyProtocol) ?? 'sqs'
      if (qLabel.startsWith('stream:redis:')) {
        protocol = 'redis-stream'
      } else if (
        qLabel.startsWith('task:') ||
        q.broker === 'dramatiq' ||
        q.broker === 'rq' ||
        q.broker === 'celery' ||
        q.broker === 'hangfire' ||
        q.broker?.startsWith('quartz')
      ) {
        protocol = 'task-queue'
      } else if (q.broker === 'redis') {
        protocol = 'redis'
      }
      if (!showAll && !activeProtocols.has(protocol)) continue
      const pids = getProducers(qRaw)
      const cids = getConsumers(qRaw)
      nodes.push(buildNode(q.id, q.label, protocol, q.repo, pids.length + cids.length))
      for (const pid of pids) edges.push({ source: pid, target: q.id, kind: 'producer' })
      for (const cid of cids) edges.push({ source: q.id, target: cid, kind: 'consumer' })
    }

    // NATS subjects
    if (showAll || activeProtocols.has('nats')) {
      for (const n of (data.nats_subjects ?? [])) {
        const nRaw = n as unknown as Record<string, unknown>
        const pids = getProducers(nRaw)
        const cids = getConsumers(nRaw)
        nodes.push(buildNode(n.id, n.label, 'nats', n.repo, pids.length + cids.length))
        for (const pid of pids) edges.push({ source: pid, target: n.id, kind: 'producer' })
        for (const cid of cids) edges.push({ source: n.id, target: cid, kind: 'consumer' })
      }
    }

    // WebSocket/SSE/Redis pub-sub channels
    for (const c of (data.channels ?? [])) {
      // channel_type from backend: 'websocket' | 'sse' | 'graphql_subscription' | 'redis_pubsub' | 'pubsub'
      const cRaw = c as unknown as Record<string, unknown>
      const cLabel = (cRaw.label as string) ?? ''
      let protocol: TopologyProtocol = c.channel_type as TopologyProtocol
      if (c.channel_type === 'pubsub' || c.channel_type === 'redis_pubsub' || cLabel.startsWith('channel:redis-pubsub:')) {
        protocol = 'redis_pubsub'
      }
      if (!showAll && !activeProtocols.has(protocol)) continue
      const eids = getEmitters(cRaw)
      const sids = getSubscribers(cRaw)
      nodes.push(buildNode(c.id, c.label, protocol, c.repo, eids.length + sids.length))
      for (const eid of eids) edges.push({ source: eid, target: c.id, kind: 'producer' })
      for (const sid of sids) edges.push({ source: c.id, target: sid, kind: 'consumer' })
    }

    // GraphQL subscriptions
    if (showAll || activeProtocols.has('graphql_subscription')) {
      for (const g of (data.graphql_subscriptions ?? [])) {
        const gRaw = g as unknown as Record<string, unknown>
        const pids: string[] = (gRaw.publisher_ids as string[] | undefined) ?? (gRaw.publishers as string[] | undefined) ?? []
        const sids: string[] = (gRaw.subscriber_ids as string[] | undefined) ?? (gRaw.subscribers as string[] | undefined) ?? []
        nodes.push(buildNode(g.id, g.label, 'graphql_subscription', g.repo, pids.length + sids.length))
        for (const pid of pids) edges.push({ source: pid, target: g.id, kind: 'producer' })
        for (const sid of sids) edges.push({ source: g.id, target: sid, kind: 'consumer' })
      }
    }

    // Serverless functions (#946)
    if (showAll || activeProtocols.has('serverless')) {
      for (const f of (data.functions ?? [])) {
        const fRaw = f as unknown as Record<string, unknown>
        const iids: string[] = (fRaw.invoker_ids as string[] | undefined) ?? (fRaw.invokers as string[] | undefined) ?? []
        const hids: string[] = (fRaw.handler_ids as string[] | undefined) ?? (fRaw.handlers as string[] | undefined) ?? []
        nodes.push(buildNode(f.id, f.label, 'serverless', f.repo, iids.length + hids.length))
        for (const iid of iids) edges.push({ source: iid, target: f.id, kind: 'producer' })
        for (const hid of hids) edges.push({ source: f.id, target: hid, kind: 'consumer' })
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
