/**
 * useAllIndexProgress — SSE-backed hook that monitors indexing progress across
 * ALL groups simultaneously.
 *
 * Subscribes to GET /api/index-progress (wildcard stream) and returns a Map
 * from group slug → { status, overall_pct, repos } so the landing page can
 * show per-card badges without opening one EventSource per card.
 *
 * Lifecycle:
 *   - Opens a single EventSource on mount; closes on unmount.
 *   - Auto-reconnects (native EventSource behaviour).
 *   - When a group transitions to 'done' the entry stays in the map for
 *     DONE_LINGER_MS so the checkmark animation plays, then is removed.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  IndexProgressState,
  RepoProgress,
  SseClosePayload,
  SseProgressPayload,
} from '@/types/indexProgress'

// ──────────────────────────────────────────────────────────────────────────────
// Constants
// ──────────────────────────────────────────────────────────────────────────────

/** How long a 'done' entry lingers in the map before being removed (ms). */
const DONE_LINGER_MS = 10_000

/** How long an 'error' entry lingers in the map before being removed (ms). */
const ERROR_LINGER_MS = 15_000

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

function computeOverallPct(repos: RepoProgress[]): number {
  if (repos.length === 0) return 0
  const totalFiles = repos.reduce((sum, r) => sum + r.files_total, 0)
  if (totalFiles === 0) {
    const doneCount = repos.filter((r) => r.phase === 'done' || r.phase === 'writing_graph').length
    return Math.round((doneCount / repos.length) * 100)
  }
  const doneFiles = repos.reduce((sum, r) => sum + r.files_done, 0)
  return Math.min(100, Math.round((doneFiles / totalFiles) * 100))
}

function mergeRepos(
  existing: Map<string, RepoProgress>,
  incoming: RepoProgress[],
): Map<string, RepoProgress> {
  const next = new Map(existing)
  for (const r of incoming) {
    next.set(r.slug, r)
  }
  return next
}

// ──────────────────────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────────────────────

export type GroupProgressMap = Map<string, IndexProgressState>

export interface UseAllIndexProgressReturn {
  /** Map from group slug → current IndexProgressState. Empty when no group is indexing. */
  progress: GroupProgressMap
  /** Manually clear a group entry (e.g. after user dismisses the card badge). */
  dismiss: (group: string) => void
}

// ──────────────────────────────────────────────────────────────────────────────
// Hook
// ──────────────────────────────────────────────────────────────────────────────

export function useAllIndexProgress(): UseAllIndexProgressReturn {
  const [progress, setProgress] = useState<GroupProgressMap>(new Map())

  // Per-group repo accumulators — kept in a ref to avoid stale-closure issues.
  const reposRef = useRef<Map<string, Map<string, RepoProgress>>>(new Map())

  // Linger timers for completed / errored groups.
  const lingerTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())

  const clearLingerTimer = useCallback((group: string) => {
    const existing = lingerTimers.current.get(group)
    if (existing) {
      clearTimeout(existing)
      lingerTimers.current.delete(group)
    }
  }, [])

  const scheduleRemoval = useCallback(
    (group: string, delayMs: number) => {
      clearLingerTimer(group)
      const id = setTimeout(() => {
        setProgress((prev) => {
          if (!prev.has(group)) return prev
          const next = new Map(prev)
          next.delete(group)
          return next
        })
        reposRef.current.delete(group)
        lingerTimers.current.delete(group)
      }, delayMs)
      lingerTimers.current.set(group, id)
    },
    [clearLingerTimer],
  )

  const dismiss = useCallback(
    (group: string) => {
      clearLingerTimer(group)
      setProgress((prev) => {
        if (!prev.has(group)) return prev
        const next = new Map(prev)
        next.delete(group)
        return next
      })
      reposRef.current.delete(group)
    },
    [clearLingerTimer],
  )

  useEffect(() => {
    const url = '/api/index-progress'
    const es = new EventSource(url)

    // ── connected (one fire per group when the daemon starts streaming) ──────
    es.addEventListener('connected', (ev: MessageEvent) => {
      try {
        const payload = JSON.parse(ev.data as string) as { group: string }
        const group = payload.group
        if (!group) return
        clearLingerTimer(group)
        if (!reposRef.current.has(group)) {
          reposRef.current.set(group, new Map())
        }
        setProgress((prev) => {
          const next = new Map(prev)
          next.set(group, {
            status: 'indexing',
            overall_pct: 0,
            repos: [],
            last_event_at: new Date().toISOString(),
          })
          return next
        })
      } catch {
        // ignore
      }
    })

    // ── progress ─────────────────────────────────────────────────────────────
    es.addEventListener('progress', (ev: MessageEvent) => {
      try {
        const payload: SseProgressPayload = JSON.parse(ev.data as string)
        const { group, repos: incoming } = payload
        if (!group) return

        let groupRepos = reposRef.current.get(group)
        if (!groupRepos) {
          groupRepos = new Map()
          reposRef.current.set(group, groupRepos)
        }
        const merged = mergeRepos(groupRepos, incoming)
        reposRef.current.set(group, merged)

        const reposList = Array.from(merged.values())
        const overall_pct = computeOverallPct(reposList)

        setProgress((prev) => {
          const next = new Map(prev)
          next.set(group, {
            status: 'indexing',
            overall_pct,
            repos: reposList,
            last_event_at: new Date().toISOString(),
          })
          return next
        })
      } catch {
        // ignore
      }
    })

    // ── heartbeat ────────────────────────────────────────────────────────────
    es.addEventListener('heartbeat', (ev: MessageEvent) => {
      try {
        const payload = JSON.parse(ev.data as string) as { group?: string }
        const group = payload.group
        if (!group) return
        setProgress((prev) => {
          if (!prev.has(group)) return prev
          const next = new Map(prev)
          const cur = prev.get(group)!
          next.set(group, { ...cur, last_event_at: new Date().toISOString() })
          return next
        })
      } catch {
        // ignore — heartbeat shape may vary
      }
    })

    // ── close ────────────────────────────────────────────────────────────────
    es.addEventListener('close', (ev: MessageEvent) => {
      try {
        const payload: SseClosePayload = JSON.parse(ev.data as string)
        const { group, status, error } = payload
        if (!group) return

        const groupRepos = reposRef.current.get(group)
        const repos = groupRepos ? Array.from(groupRepos.values()) : []

        setProgress((prev) => {
          const next = new Map(prev)
          next.set(group, {
            status,
            overall_pct: status === 'done' ? 100 : computeOverallPct(repos),
            repos,
            last_event_at: new Date().toISOString(),
            error,
          })
          return next
        })

        scheduleRemoval(group, status === 'done' ? DONE_LINGER_MS : ERROR_LINGER_MS)
      } catch {
        // ignore
      }
    })

    // ── network error ─────────────────────────────────────────────────────────
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        // Connection fully lost — mark all in-flight groups as error.
        setProgress((prev) => {
          if (prev.size === 0) return prev
          const next = new Map<string, IndexProgressState>()
          for (const [group, state] of prev.entries()) {
            if (state.status === 'indexing' || state.status === 'connecting') {
              next.set(group, {
                ...state,
                status: 'error',
                error: 'Connection to daemon lost.',
                last_event_at: new Date().toISOString(),
              })
              scheduleRemoval(group, ERROR_LINGER_MS)
            } else {
              next.set(group, state)
            }
          }
          return next
        })
      }
    }

    return () => {
      es.close()
      // Clear all linger timers on unmount.
      for (const id of lingerTimers.current.values()) {
        clearTimeout(id)
      }
      lingerTimers.current.clear()
    }
  }, [clearLingerTimer, scheduleRemoval])

  return { progress, dismiss }
}
