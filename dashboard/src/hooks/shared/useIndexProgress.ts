/**
 * useIndexProgress — SSE-backed reactive hook for indexing progress.
 *
 * Streams from GET /api/index-progress/{groupSlug} (or /api/index-progress
 * when groupSlug is undefined) and aggregates per-repo snapshots into a
 * single IndexProgressState.
 *
 * Lifecycle:
 *   - Opens an EventSource when groupSlug is provided (non-empty string).
 *   - Reconnects automatically on transient errors (browser-native ES behaviour).
 *   - Closes and cleans up on component unmount or groupSlug change.
 *   - Exposes a manual `reset()` to return to idle (e.g. after modal closes).
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  IndexProgressState,
  RepoProgress,
  SseClosePayload,
  SseProgressPayload,
} from '@/types/indexProgress'

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

/** Compute 0–100 overall percentage, weighted by files_total. */
function computeOverallPct(repos: RepoProgress[]): number {
  if (repos.length === 0) return 0

  const totalFiles = repos.reduce((sum, r) => sum + r.files_total, 0)
  if (totalFiles === 0) {
    // All repos have no file count yet — return simple phase-based average.
    const doneCount = repos.filter(
      (r) => r.phase === 'done' || r.phase === 'writing_graph',
    ).length
    return Math.round((doneCount / repos.length) * 100)
  }

  const doneFiles = repos.reduce((sum, r) => sum + r.files_done, 0)
  return Math.min(100, Math.round((doneFiles / totalFiles) * 100))
}

/** Merge an incoming batch of RepoProgress records into the existing map. */
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
// Hook
// ──────────────────────────────────────────────────────────────────────────────

const INITIAL_STATE: IndexProgressState = {
  status: 'idle',
  overall_pct: 0,
  repos: [],
}

export interface UseIndexProgressReturn extends IndexProgressState {
  /** Manually reset to idle (e.g. when the modal is dismissed after completion). */
  reset: () => void
}

/**
 * @param groupSlug - slug of the group to monitor; pass undefined/'' to skip.
 */
export function useIndexProgress(groupSlug: string | undefined): UseIndexProgressReturn {
  const [state, setState] = useState<IndexProgressState>(INITIAL_STATE)

  // Keep a ref to the EventSource so the cleanup function always has the
  // current instance regardless of stale closures.
  const esRef = useRef<EventSource | null>(null)

  // Repos accumulator lives in a ref — we don't want intermediate updates
  // to trigger re-renders on every repo merge; we push to state in the
  // progress handler at the EventSource event rate.
  const reposRef = useRef<Map<string, RepoProgress>>(new Map())

  const reset = useCallback(() => {
    reposRef.current = new Map()
    setState(INITIAL_STATE)
  }, [])

  useEffect(() => {
    if (!groupSlug) return

    // Reset accumulator when groupSlug changes.
    reposRef.current = new Map()

    const url = `/api/index-progress/${encodeURIComponent(groupSlug)}`
    const es = new EventSource(url)
    esRef.current = es

    setState((prev) => ({ ...prev, status: 'connecting' }))

    // ── connected ──────────────────────────────────────────────────────────
    es.addEventListener('connected', () => {
      setState((prev) => ({
        ...prev,
        status: 'indexing',
        last_event_at: new Date().toISOString(),
      }))
    })

    // ── progress ───────────────────────────────────────────────────────────
    es.addEventListener('progress', (ev: MessageEvent) => {
      try {
        const payload: SseProgressPayload = JSON.parse(ev.data as string)
        const updated = mergeRepos(reposRef.current, payload.repos)
        reposRef.current = updated

        const repos = Array.from(updated.values())
        const overall_pct = computeOverallPct(repos)

        setState({
          status: 'indexing',
          overall_pct,
          repos,
          last_event_at: new Date().toISOString(),
        })
      } catch {
        // Malformed JSON — ignore and wait for next event.
      }
    })

    // ── heartbeat ──────────────────────────────────────────────────────────
    es.addEventListener('heartbeat', () => {
      setState((prev) => ({
        ...prev,
        last_event_at: new Date().toISOString(),
      }))
    })

    // ── close ──────────────────────────────────────────────────────────────
    es.addEventListener('close', (ev: MessageEvent) => {
      try {
        const payload: SseClosePayload = JSON.parse(ev.data as string)
        const repos = Array.from(reposRef.current.values())
        setState({
          status: payload.status,
          overall_pct: payload.status === 'done' ? 100 : computeOverallPct(repos),
          repos,
          last_event_at: new Date().toISOString(),
          error: payload.error,
        })
      } catch {
        setState((prev) => ({ ...prev, status: 'done', overall_pct: 100 }))
      }
      es.close()
    })

    // ── network error ──────────────────────────────────────────────────────
    es.onerror = () => {
      // EventSource auto-reconnects; only move to error if it's in CLOSED state.
      if (es.readyState === EventSource.CLOSED) {
        setState((prev) => ({
          ...prev,
          status: 'error',
          error: 'Connection to daemon lost.',
          last_event_at: new Date().toISOString(),
        }))
      }
    }

    return () => {
      es.close()
      esRef.current = null
    }
  }, [groupSlug])

  return { ...state, reset }
}
