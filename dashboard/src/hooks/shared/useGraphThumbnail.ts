/**
 * useGraphThumbnail — fetches the layout-snapshot for landing card thumbnails.
 *
 * GET /api/graph/{group}/layout-snapshot?top=200
 *
 * Stale-while-revalidate: 5 min staleTime, 10 min gcTime.
 * Suspense is NOT used — the component renders a skeleton while loading.
 *
 * On 404 (group not yet indexed) the hook returns an empty nodes array so the
 * card can show a "no preview" placeholder gracefully.
 */

import { useQuery } from '@tanstack/react-query'
import { fetchGraphThumbnail } from '@/api/client'
import type { GraphThumbnailResponse } from '@/types/api'

export function useGraphThumbnail(group: string, enabled = true) {
  return useQuery<GraphThumbnailResponse, Error>({
    queryKey: ['graph-thumbnail', group],
    queryFn: () => fetchGraphThumbnail(group),
    staleTime: 5 * 60 * 1000,   // 5 min — snapshot only changes on re-index
    gcTime:    10 * 60 * 1000,  // 10 min LRU
    enabled,
    // Don't retry on 404 — the group is simply not indexed yet.
    retry: (failureCount, err) => {
      if (err instanceof Error && 'status' in err && (err as { status: number }).status === 404) {
        return false
      }
      return failureCount < 2
    },
  })
}
