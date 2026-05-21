/**
 * useGraphHighlight — reserved hook interface for future Jarvis MCP query
 * visualization (epic #1157).
 *
 * When an external AI agent queries the archigraph MCP server, this hook
 * will receive the returned node IDs and edge IDs via a Server-Sent Events
 * subscription, then drive Cosmograph's selectPoints() API to highlight
 * them in real-time — a "brain thinking" effect showing what the model
 * is consuming as it consumes it.
 *
 * Today this is a NO-OP stub that establishes the interface so:
 *   1. GraphCanvas.tsx can wire the `forceVisibleIds` parameter without
 *      needing a second PR.
 *   2. LoD computation already respects `forceVisibleIds` so highlighted
 *      nodes are NEVER pruned by zoom-LoD filtering (#1120 constraint).
 *   3. selectPoints() is called through this hook instead of directly from
 *      the color mode effect, keeping highlight state decoupled from base
 *      color mode — required by #1157 design.
 *
 * Implementation phases (future, NOT this PR):
 *   Phase 1 — SSE subscription to GET /api/mcp-activity/stream
 *   Phase 2 — selectPoints() pulsing on returned nodes + edge flash
 *   Phase 3 — multi-agent color disambiguation + activity log sidebar
 *
 * @returns highlight state and imperative controls
 */

export type HighlightSource = 'mcp-query' | 'manual' | null

export interface GraphHighlightState {
  /** Node IDs currently highlighted (empty = no active highlight) */
  highlightedNodeIds: ReadonlySet<string>
  /** Edge IDs currently highlighted */
  highlightedEdgeIds: ReadonlySet<string>
  /** Who set the highlight (for multi-agent disambiguation in Phase 3) */
  source: HighlightSource
  /** Whether the highlight is actively animating */
  isActive: boolean
}

export interface GraphHighlightControls {
  /**
   * Highlight a set of node + edge IDs temporarily.
   * The highlight decays after `durationMs` (default 5000ms).
   * Calling this while a highlight is active replaces it.
   */
  highlight(nodeIds: string[], edgeIds?: string[], durationMs?: number): void
  /** Immediately clear any active highlight. */
  clear(): void
}

export type UseGraphHighlightReturn = GraphHighlightState & GraphHighlightControls

const EMPTY_SET = new Set<string>()

/**
 * Stub implementation — always returns an inactive highlight with no-op controls.
 *
 * Replace the body of this hook in Phase 1 with the real SSE subscription logic.
 * The interface (return type) must remain stable.
 */
export function useGraphHighlight(): UseGraphHighlightReturn {
  return {
    highlightedNodeIds: EMPTY_SET,
    highlightedEdgeIds: EMPTY_SET,
    source: null,
    isActive: false,
    highlight: _noop,
    clear: _noop,
  }
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars
function _noop(..._args: unknown[]): void {
  // Phase 1 will replace this with the SSE subscription + selectPoints() call.
  // Left empty intentionally — no console.warn so prod builds stay silent.
}
