/**
 * useEnrichmentProgress — polling hook for per-tier enrichment job progress (#1286).
 *
 * Polls GET /api/enrichments/{group}/progress every 3 seconds while any tier
 * has running or queued jobs. Stops automatically when all tiers are done.
 *
 * Returns the latest EnrichmentProgressResponse plus a boolean `isActive`
 * indicating whether any enrichment is currently in progress.
 */

import { useEffect, useRef, useState } from 'react'
import { fetchEnrichmentProgress } from '@/api/client'
import type { EnrichmentProgressResponse } from '@/types/api'

export const POLL_INTERVAL_MS = 3_000

export interface UseEnrichmentProgressReturn {
  progress: EnrichmentProgressResponse | null
  /** True while any tier has running or queued jobs. */
  isActive: boolean
  /** True on the initial fetch before any data arrives. */
  isLoading: boolean
  error: string | null
}

function hasActiveJobs(data: EnrichmentProgressResponse): boolean {
  return data.tiers.some((t) => t.running > 0 || t.queued > 0)
}

/**
 * @param group - The group slug to monitor. Pass undefined/'' to skip.
 */
export function useEnrichmentProgress(group: string | undefined): UseEnrichmentProgressReturn {
  const [progress, setProgress] = useState<EnrichmentProgressResponse | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    if (!group) return

    let cancelled = false

    async function poll() {
      if (cancelled || !mountedRef.current) return
      try {
        const data = await fetchEnrichmentProgress(group!)
        if (cancelled || !mountedRef.current) return
        setProgress(data)
        setError(null)

        // Keep polling while jobs are active; back off to a single static
        // snapshot when all tiers are idle (total == 0 or all done).
        const active = hasActiveJobs(data)
        if (active) {
          timerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
        }
      } catch (err) {
        if (cancelled || !mountedRef.current) return
        setError(err instanceof Error ? err.message : 'Failed to fetch progress')
        // Retry on error so a transient daemon restart doesn't freeze the UI.
        timerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
      } finally {
        if (!cancelled && mountedRef.current) {
          setIsLoading(false)
        }
      }
    }

    // Initial fetch
    setIsLoading(true)
    void poll()

    return () => {
      cancelled = true
      if (timerRef.current != null) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [group])

  const isActive = progress != null && hasActiveJobs(progress)

  return { progress, isActive, isLoading, error }
}
