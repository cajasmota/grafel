/**
 * Types for the SSE-backed indexing progress stream.
 *
 * The daemon streams events on:
 *   GET /api/index-progress/{group}   — single group
 *   GET /api/index-progress           — all groups
 *
 * Event types: connected | progress | heartbeat | close
 */

// ──────────────────────────────────────────────────────────────────────────────
// Phase enum (mirrors internal/indexer phases from sub-A instrumentation)
// ──────────────────────────────────────────────────────────────────────────────

export type IndexPhase =
  | 'waiting'
  | 'scanning'
  | 'extracting_ast'
  | 'resolving_refs'
  | 'writing_graph'
  | 'done'
  | 'error'

// ──────────────────────────────────────────────────────────────────────────────
// Per-repo progress snapshot (shape of `data` in a `progress` SSE event)
// ──────────────────────────────────────────────────────────────────────────────

export interface RepoProgress {
  slug: string
  phase: IndexPhase
  files_done: number
  files_total: number
  /** Estimated ms remaining — absent during scanning or when unknown. */
  eta_ms?: number
  /** Absolute path of file currently being processed (extracting_ast phase). */
  current_file?: string
  /** ISO timestamp when this phase started — used to derive elapsed time. */
  phase_started_at?: string
  /** Present only when phase === 'error'. */
  error?: string
}

// ──────────────────────────────────────────────────────────────────────────────
// Aggregated state returned by useIndexProgress
// ──────────────────────────────────────────────────────────────────────────────

export type IndexStatus = 'idle' | 'connecting' | 'indexing' | 'done' | 'error'

export interface IndexProgressState {
  status: IndexStatus
  /** 0–100, averaged across all repos weighted by files_total. */
  overall_pct: number
  repos: RepoProgress[]
  /** ISO timestamp of last SSE event (for heartbeat freshness). */
  last_event_at?: string
  /** Top-level error message when status === 'error'. */
  error?: string
}

// ──────────────────────────────────────────────────────────────────────────────
// Raw SSE payload shapes (unmarshalled from event.data JSON)
// ──────────────────────────────────────────────────────────────────────────────

export interface SseConnectedPayload {
  group: string
}

export interface SseProgressPayload {
  group: string
  repos: RepoProgress[]
}

export interface SseClosePayload {
  group: string
  status: 'done' | 'error'
  error?: string
}
