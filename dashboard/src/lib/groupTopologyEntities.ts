/**
 * Grouping utility for Surface 3 — Broker Topology list view.
 *
 * Supports two grouping strategies:
 *  - 'repo'     — group by the entity's source repo
 *  - 'protocol' — group by broker / channel protocol
 *
 * Each entity across topics, queues, nats_subjects, channels, and
 * graphql_subscriptions is normalised into a flat TopologyListRow, then
 * partitioned into named TopologyEntityGroup buckets sorted alphabetically.
 */

import type {
  TopologyResponse,
  TopologyProtocol,
  TopicNode,
  QueueNode,
  NatsSubject,
  ChannelNode,
  GraphQLSubscription,
  FunctionNode,
} from '@/types/api'
import { PROTOCOL_COLORS } from '@/lib/colors'

// ────────────────────────────────────────────────────────────────────────────
// Broker-level grouping (#1142)
// ────────────────────────────────────────────────────────────────────────────

export interface BrokerGroupHealth {
  /** Total topics/queues in this broker */
  total: number
  /** Topics with ≥1 consumer (active) */
  active: number
  /** Topics with 0 consumers (orphan publishers) */
  orphanPublishers: number
  /** Topics with 0 producers (orphan subscribers) */
  orphanSubscribers: number
}

export interface ServiceSubGroup {
  service: string
  rows: TopologyListRow[]
}

export interface BrokerGroup {
  /** Protocol slug — used for localStorage key */
  slug: TopologyProtocol
  /** Human-readable broker name */
  label: string
  /** All rows for this broker (flattened, across services) */
  rows: TopologyListRow[]
  /** Rows grouped by service/repo */
  services: ServiceSubGroup[]
  /** Health summary */
  health: BrokerGroupHealth
}

// ────────────────────────────────────────────────────────────────────────────
// Canonical row shape
// ────────────────────────────────────────────────────────────────────────────

export interface TopologyListRow {
  id: string
  label: string
  protocol: TopologyProtocol
  protocolLabel: string
  repo: string
  producerCount: number
  consumerCount: number
}

export interface TopologyEntityGroup {
  name: string
  rows: TopologyListRow[]
}

// ────────────────────────────────────────────────────────────────────────────
// Normalisation helpers
// ────────────────────────────────────────────────────────────────────────────

function fromTopic(t: TopicNode): TopologyListRow {
  return {
    id: t.id,
    label: t.label,
    protocol: t.broker as TopologyProtocol,
    protocolLabel: PROTOCOL_COLORS[t.broker as TopologyProtocol]?.label ?? t.broker,
    repo: t.repo,
    producerCount: (t.producer_ids ?? []).length,
    consumerCount: (t.consumer_ids ?? []).length,
  }
}

function fromQueue(q: QueueNode): TopologyListRow {
  // #1116: Task/ScheduledJob entities arrive with an empty broker but a non-empty
  // framework property. Fall back to the 'task-queue' protocol so PROTOCOL_COLORS
  // lookup always returns a defined spec (avoids .hex crash in TopicNode/TopologyList).
  const protocol: TopologyProtocol =
    q.broker && (q.broker as string) !== ''
      ? (q.broker as TopologyProtocol)
      : 'task-queue'
  return {
    id: q.id,
    label: q.label,
    protocol,
    protocolLabel: PROTOCOL_COLORS[protocol]?.label ?? (q.broker || 'Task Queue'),
    repo: q.repo,
    producerCount: (q.producer_ids ?? []).length,
    consumerCount: (q.consumer_ids ?? []).length,
  }
}

function fromNatsSubject(n: NatsSubject): TopologyListRow {
  return {
    id: n.id,
    label: n.label,
    protocol: 'nats',
    protocolLabel: PROTOCOL_COLORS.nats.label,
    repo: n.repo,
    producerCount: (n.producer_ids ?? []).length,
    consumerCount: (n.consumer_ids ?? []).length,
  }
}

function fromChannel(c: ChannelNode): TopologyListRow {
  return {
    id: c.id,
    label: c.label,
    protocol: c.channel_type as TopologyProtocol,
    protocolLabel: PROTOCOL_COLORS[c.channel_type as TopologyProtocol]?.label ?? c.channel_type,
    repo: c.repo,
    producerCount: (c.emitter_ids ?? []).length,
    consumerCount: (c.subscriber_ids ?? []).length,
  }
}

function fromGraphQLSubscription(g: GraphQLSubscription): TopologyListRow {
  return {
    id: g.id,
    label: g.label,
    protocol: 'graphql_subscription',
    protocolLabel: PROTOCOL_COLORS.graphql_subscription.label,
    repo: g.repo,
    producerCount: (g.publisher_ids ?? []).length,
    consumerCount: (g.subscriber_ids ?? []).length,
  }
}

function fromFunction(f: FunctionNode): TopologyListRow {
  return {
    id: f.id,
    label: f.label,
    protocol: 'serverless',
    protocolLabel: PROTOCOL_COLORS.serverless?.label ?? 'Serverless',
    repo: f.repo,
    producerCount: (f.invoker_ids ?? []).length,
    consumerCount: (f.handler_ids ?? []).length,
  }
}

// ────────────────────────────────────────────────────────────────────────────
// Public API
// ────────────────────────────────────────────────────────────────────────────

export type TopologyGrouping = 'repo' | 'protocol'

/**
 * Flatten all topology entities from a TopologyResponse into TopologyListRows,
 * optionally filtering by a text query (case-insensitive substring on label).
 */
export function flattenTopologyEntities(
  data: TopologyResponse,
  query = '',
): TopologyListRow[] {
  const rows: TopologyListRow[] = [
    ...data.topics.map(fromTopic),
    ...data.queues.map(fromQueue),
    ...data.nats_subjects.map(fromNatsSubject),
    ...data.channels.map(fromChannel),
    ...data.graphql_subscriptions.map(fromGraphQLSubscription),
    ...(data.functions ?? []).map(fromFunction),
  ]

  if (!query.trim()) return rows

  const q = query.toLowerCase().trim()
  return rows.filter((r) => r.label.toLowerCase().includes(q))
}

export type BrokerSortField = 'name' | 'producers' | 'consumers'

/**
 * Group all topology rows by broker (protocol), then by service/repo within each broker.
 * Used for the "By broker" grouping in the All tab (#1142).
 *
 * Sort order within a broker section is controlled by `sortBy`.
 */
export function groupTopologyByBroker(
  rows: TopologyListRow[],
  sortBy: BrokerSortField = 'name',
): BrokerGroup[] {
  // Accumulate rows per broker slug
  const byBroker = new Map<TopologyProtocol, TopologyListRow[]>()
  for (const row of rows) {
    const bucket = byBroker.get(row.protocol) ?? []
    bucket.push(row)
    byBroker.set(row.protocol, bucket)
  }

  const groups: BrokerGroup[] = []

  for (const [slug, brokerRows] of byBroker.entries()) {
    const spec = PROTOCOL_COLORS[slug]
    const label = spec?.label ?? slug

    // Sort rows within broker section
    const sortedRows = [...brokerRows].sort((a, b) => {
      if (sortBy === 'producers') return b.producerCount - a.producerCount
      if (sortBy === 'consumers') return b.consumerCount - a.consumerCount
      return a.label.localeCompare(b.label)
    })

    // Build per-service sub-groups
    const byService = new Map<string, TopologyListRow[]>()
    for (const row of sortedRows) {
      const svc = row.repo || 'unknown'
      const svcBucket = byService.get(svc) ?? []
      svcBucket.push(row)
      byService.set(svc, svcBucket)
    }
    const services: ServiceSubGroup[] = []
    for (const [service, svcRows] of byService.entries()) {
      services.push({ service, rows: svcRows })
    }
    // Sort services alphabetically
    services.sort((a, b) => a.service.localeCompare(b.service))

    // Health summary
    const orphanPublishers = sortedRows.filter((r) => r.consumerCount === 0).length
    const orphanSubscribers = sortedRows.filter((r) => r.producerCount === 0).length
    const active = sortedRows.filter((r) => r.producerCount > 0 && r.consumerCount > 0).length

    groups.push({
      slug,
      label,
      rows: sortedRows,
      services,
      health: {
        total: sortedRows.length,
        active,
        orphanPublishers,
        orphanSubscribers,
      },
    })
  }

  // Sort broker groups by total count descending (most active broker first)
  groups.sort((a, b) => b.rows.length - a.rows.length)

  return groups
}

/**
 * Group a flat list of TopologyListRows by repo or protocol.
 * Groups are sorted alphabetically; rows within each group are sorted by name.
 */
export function groupTopologyEntities(
  rows: TopologyListRow[],
  by: TopologyGrouping,
): TopologyEntityGroup[] {
  const map = new Map<string, TopologyListRow[]>()

  for (const row of rows) {
    const key = by === 'repo' ? row.repo : row.protocolLabel
    const bucket = map.get(key) ?? []
    bucket.push(row)
    map.set(key, bucket)
  }

  const groups: TopologyEntityGroup[] = []
  for (const [name, items] of map.entries()) {
    groups.push({
      name,
      rows: [...items].sort((a, b) => a.label.localeCompare(b.label)),
    })
  }

  return groups.sort((a, b) => a.name.localeCompare(b.name))
}
