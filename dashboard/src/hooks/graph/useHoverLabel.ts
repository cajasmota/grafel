import { useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchGraphLabels } from '@/api/client'

/**
 * Tier 2 hover-to-label: when a node is hovered whose label equals its id
 * (i.e. it was not in the initial top-200 label fetch), fetch its label lazily.
 *
 * Uses staleTime=5min to match the main graph query, and seeds the result
 * back into the 'graph-labels' query cache so subsequent renders pick it up
 * without extra round-trips.
 *
 * Returns the label string for the hovered node, or undefined while loading.
 */
export function useHoverLabel(
  group: string,
  hoveredNodeId: string | null,
  /**
   * Pass the current label for the hovered node so we only fire the fetch
   * when the label is still the raw-id fallback (meaning the node was NOT
   * in the initial top-200 batch).
   */
  currentLabel: string | undefined,
): string | undefined {
  const queryClient = useQueryClient()

  // Only fetch when the label is missing (undefined) or equal to the id
  // — that's the sentinel set by useGraphData for unlabelled nodes.
  const needsFetch = !!hoveredNodeId && (currentLabel === undefined || currentLabel === hoveredNodeId)

  const { data } = useQuery({
    queryKey: ['hover-label', group, hoveredNodeId],
    queryFn: async () => {
      const res = await fetchGraphLabels(group, { ids: [hoveredNodeId!] })
      // Seed into the labels cache so the main node list picks it up on next render.
      queryClient.setQueryData<{ labels: Array<{ id: string; label: string }> }>(
        ['graph-labels', group],
        (old) => {
          if (!old) return old
          const entry = res.labels[0]
          if (!entry) return old
          // Avoid duplicates — replace existing entry if present.
          const rest = old.labels.filter((l) => l.id !== entry.id)
          return { labels: [...rest, entry] }
        },
      )
      return res
    },
    staleTime: 5 * 60 * 1000,
    enabled: needsFetch,
  })

  const label = data?.labels[0]?.label
  return needsFetch ? label : currentLabel
}
