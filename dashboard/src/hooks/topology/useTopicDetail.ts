import { useMemo } from 'react'
import type { TopologyResponse, TopicNode, QueueNode, NatsSubject, ChannelNode, GraphQLSubscription, TopologyEntityStub } from '@/types/api'

export type AnyTopologyNode = TopicNode | QueueNode | NatsSubject | ChannelNode | GraphQLSubscription

export interface TopicDetailData {
  node: AnyTopologyNode | null
  producers: TopologyEntityStub[]
  consumers: TopologyEntityStub[]
  /** For topic→topic transforms */
  transformsTo: Array<TopicNode | QueueNode | NatsSubject>
}

/**
 * Derives per-topic/queue/channel detail from already-fetched topology data.
 * No additional fetch — uses the entity stubs from the topology response.
 */
export function useTopicDetail(
  topicId: string | null,
  data: TopologyResponse | undefined,
): TopicDetailData {
  return useMemo<TopicDetailData>(() => {
    if (!topicId || !data) return { node: null, producers: [], consumers: [], transformsTo: [] }

    // Find the node across all collections
    const allNodes: AnyTopologyNode[] = [
      ...data.topics,
      ...data.queues,
      ...data.nats_subjects,
      ...data.channels,
      ...data.graphql_subscriptions,
    ]
    const node = allNodes.find((n) => n.id === topicId) ?? null

    if (!node) return { node: null, producers: [], consumers: [], transformsTo: [] }

    // Resolve producer/consumer stubs
    const resolveIds = (ids: string[]): TopologyEntityStub[] =>
      ids.flatMap((id) => {
        const stub = data.producers[id] ?? data.consumers[id]
        return stub ? [stub] : []
      })

    let producerIds: string[] = []
    let consumerIds: string[] = []

    if ('producer_ids' in node) producerIds = node.producer_ids
    if ('consumer_ids' in node) consumerIds = node.consumer_ids
    if ('emitter_ids' in node) producerIds = (node as ChannelNode).emitter_ids
    if ('subscriber_ids' in node) consumerIds = (node as ChannelNode | GraphQLSubscription).subscriber_ids
    if ('publisher_ids' in node) producerIds = (node as GraphQLSubscription).publisher_ids

    // Resolve topic→topic transforms
    const transformIds = 'transforms_to' in node ? (node as TopicNode).transforms_to : []
    const allBrokerNodes = [...data.topics, ...data.queues, ...data.nats_subjects]
    const transformsTo = transformIds.flatMap((id) => {
      const found = allBrokerNodes.find((n) => n.id === id)
      return found ? [found] : []
    })

    return {
      node,
      producers: resolveIds(producerIds),
      consumers: resolveIds(consumerIds),
      transformsTo,
    }
  }, [topicId, data])
}
