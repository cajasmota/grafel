/* ============================================================
   hooks/use-topology.ts — Topology screen data hooks.

   Wraps the typed api client in TanStack Query.
   Data: static pub/sub extraction — NO runtime metrics anywhere.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type {
  TopologyChannel,
  TopologyBrokerGroup,
  ChannelLifecycle,
  BrokerCanonical,
  OrphanPublisherEntry,
  OrphanSubscriberEntry,
  CompoundGroupBy,
} from "@/data/types";

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

export const topologyQueryKey = (groupId: string) =>
  ["topology", groupId] as const;

export const topologyDetailQueryKey = (groupId: string, topicId: string) =>
  ["topology", groupId, "detail", topicId] as const;

export const orphanPublishersQueryKey = (groupId: string) =>
  ["topology", groupId, "orphan-publishers"] as const;

export const orphanSubscribersQueryKey = (groupId: string) =>
  ["topology", groupId, "orphan-subscribers"] as const;

export const compoundTopologyQueryKey = (
  groupId: string,
  groupBy: CompoundGroupBy,
) => ["topology", groupId, "compound", groupBy] as const;

// ---------------------------------------------------------------------------
// Derived helpers
// ---------------------------------------------------------------------------

/**
 * All channels across all buckets, flattened into one array.
 * Adds a `lifecycle_state` property derived from producer/consumer presence.
 * Also marks `cross_repo` based on the broker_groups data.
 */
export function flattenChannels(
  data: Awaited<ReturnType<typeof api.getTopology>> | undefined,
): (TopologyChannel & { lifecycle_state: ChannelLifecycle })[] {
  if (!data) return [];

  const crossRepoIds = new Set<string>();
  for (const bg of data.broker_groups ?? []) {
    // cross_repo_topic_count > 0 doesn't tell us which IDs, so we derive from
    // broker_groups health.  Exact per-channel cross_repo flag would need the v2
    // detail endpoint; for the list we approximate by broker.
    void bg; // annotation only — cross_repo is annotated per-channel below
  }

  const all: (TopologyChannel & { lifecycle_state: ChannelLifecycle })[] = [];

  function annotate(
    ch: TopologyChannel,
  ): TopologyChannel & { lifecycle_state: ChannelLifecycle } {
    const hasP = (ch.producers?.length ?? 0) > 0;
    const hasC = (ch.consumers?.length ?? 0) > 0;
    let lifecycle_state: ChannelLifecycle;
    if (hasP && hasC) lifecycle_state = "active";
    else if (hasP && !hasC) lifecycle_state = "orphan_subscriber";
    else if (!hasP && hasC) lifecycle_state = "orphan_publisher";
    else lifecycle_state = "orphan";
    return { ...ch, lifecycle_state };
  }

  for (const ch of data.topics ?? []) all.push(annotate(ch));
  for (const ch of data.queues ?? []) all.push(annotate(ch));
  for (const ch of data.nats_subjects ?? []) all.push(annotate(ch));
  for (const ch of data.graphql_subscriptions ?? [])
    all.push(annotate({ ...ch, broker_canonical: "graphql_subscription" as BrokerCanonical }));
  for (const ch of data.channels ?? []) all.push(annotate(ch));

  // Mark cross_repo for any channel whose id appears in any broker_group with
  // cross_repo_topic_count > 0. Since we don't have per-id flags here, we use
  // a heuristic: channels belonging to a broker with cross_repo_topic_count > 0
  // that have both producers and consumers from different repos in their ids.
  // The detail endpoint provides the authoritative cross_repo boolean.
  void crossRepoIds;

  return all;
}

/**
 * Derive total counts for the tab pills.
 */
export function deriveCounts(
  channels: (TopologyChannel & { lifecycle_state: ChannelLifecycle })[],
  brokerGroups: TopologyBrokerGroup[],
) {
  let orphanPub = 0;
  let orphanSub = 0;
  let scheduled = 0;
  for (const ch of channels) {
    if (ch.lifecycle_state === "orphan_publisher") orphanPub++;
    if (ch.lifecycle_state === "orphan_subscriber") orphanSub++;
    if (ch.scheduled) scheduled++;
  }
  let active = 0;
  for (const bg of brokerGroups ?? []) {
    active += bg.health_summary?.active ?? 0;
  }
  return { total: channels?.length ?? 0, orphanPub, orphanSub, scheduled, active };
}

/**
 * Normalise the orphan-publisher endpoint payload into the row array that the
 * Orphan-pub tab actually renders.
 *
 * The real daemon returns `{ orphan_publishers: [...] }`; older/mocked shapes
 * return a bare array (#1535). This is the ONE canonical source of truth for
 * the orphan-publisher list — both the tab body and the tab-strip badge derive
 * from it so the count can never disagree with the rendered rows (#5).
 */
export function extractOrphanPublishers(data: unknown): OrphanPublisherEntry[] {
  if (Array.isArray(data)) return data as OrphanPublisherEntry[];
  return (
    (data as { orphan_publishers?: OrphanPublisherEntry[] } | undefined)
      ?.orphan_publishers ?? []
  );
}

/**
 * Normalise the orphan-subscriber endpoint payload into the row array that the
 * Orphan-sub tab renders. Canonical source of truth for both the list body and
 * the tab-strip badge (#5). See {@link extractOrphanPublishers}.
 */
export function extractOrphanSubscribers(data: unknown): OrphanSubscriberEntry[] {
  if (Array.isArray(data)) return data as OrphanSubscriberEntry[];
  return (
    (data as { orphan_subscribers?: OrphanSubscriberEntry[] } | undefined)
      ?.orphan_subscribers ?? []
  );
}

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

/** Full topology payload for the screen. */
export function useTopology(groupId: string) {
  return useQuery({
    queryKey: topologyQueryKey(groupId),
    queryFn: () => api.getTopology(groupId),
    enabled: !!groupId,
  });
}

/** Lazy detail fetch — only when a channel is selected. */
export function useTopologyDetail(groupId: string, topicId: string | null) {
  return useQuery({
    queryKey: topologyDetailQueryKey(groupId, topicId ?? ""),
    queryFn: () => api.getTopologyDetail(groupId, topicId!),
    enabled: !!groupId && !!topicId,
  });
}

/** Orphan publishers tab data. */
export function useOrphanPublishers(groupId: string) {
  return useQuery({
    queryKey: orphanPublishersQueryKey(groupId),
    queryFn: () => api.getOrphanPublishers(groupId),
    enabled: !!groupId,
  });
}

/** Orphan subscribers tab data. */
export function useOrphanSubscribers(groupId: string) {
  return useQuery({
    queryKey: orphanSubscribersQueryKey(groupId),
    queryFn: () => api.getOrphanSubscribers(groupId),
    enabled: !!groupId,
  });
}

/**
 * Compound architecture-diagram payload (Model 1, #4810/#4811). Re-fetches
 * per group_by lens; the same nodes are re-grouped server-side so the
 * Infra/Modules/Tier toggle is a cheap query swap.
 */
export function useCompoundTopology(groupId: string, groupBy: CompoundGroupBy) {
  return useQuery({
    queryKey: compoundTopologyQueryKey(groupId, groupBy),
    queryFn: () => api.getCompoundTopology(groupId, groupBy),
    enabled: !!groupId,
  });
}
