import { useSearchParams } from 'react-router-dom'
import type { TopologyProtocol } from '@/types/api'

const ALL_PROTOCOLS: TopologyProtocol[] = [
  // Broker protocols
  'kafka',
  'rabbitmq',
  'sqs',
  'pubsub',
  'nats',
  // Channel protocols
  'websocket',
  'sse',
  'graphql_subscription',
  // New runtime entities (#946)
  'redis',
  'redis-stream',
  'redis_pubsub',
  'task-queue',
  'serverless',
]

/**
 * Reads and writes protocol filter state from the URL `protocol` param.
 * An empty set means "show all". The URL param is a comma-separated list.
 * Keeps URL as single source of truth for deep-link shareability.
 */
export function useProtocolFilters(): {
  activeProtocols: Set<TopologyProtocol>
  allProtocols: TopologyProtocol[]
  isAllActive: boolean
  toggle: (protocol: TopologyProtocol) => void
  setAll: () => void
  clearAll: () => void
  selectedTopic: string | null
  setSelectedTopic: (id: string | null) => void
} {
  const [params, setParams] = useSearchParams()

  const rawProtocol = params.get('protocol')
  const activeProtocols: Set<TopologyProtocol> = rawProtocol
    ? new Set(rawProtocol.split(',').filter(Boolean) as TopologyProtocol[])
    : new Set()

  const selectedTopic = params.get('topic') ?? null
  const isAllActive = activeProtocols.size === 0

  function update(patch: Record<string, string | null>) {
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      for (const [k, v] of Object.entries(patch)) {
        if (v === null || v === '') {
          next.delete(k)
        } else {
          next.set(k, v)
        }
      }
      return next
    })
  }

  function toggle(protocol: TopologyProtocol) {
    const next = new Set(activeProtocols)
    if (next.has(protocol)) {
      next.delete(protocol)
    } else {
      next.add(protocol)
    }
    update({ protocol: next.size > 0 ? [...next].join(',') : null })
  }

  function setAll() {
    update({ protocol: null })
  }

  function clearAll() {
    // "clear all" in context = show all (empty = no filter)
    update({ protocol: null })
  }

  return {
    activeProtocols,
    allProtocols: ALL_PROTOCOLS,
    isAllActive,
    toggle,
    setAll,
    clearAll,
    selectedTopic,
    setSelectedTopic: (id) => update({ topic: id }),
  }
}
