/**
 * EnrichmentCostModal — pre-run cost estimator dialog (#1287).
 *
 * Shows a per-tier breakdown of estimated token counts and USD cost before
 * the user triggers a batched enrichment run. The user must confirm before
 * any LLM calls are dispatched.
 *
 * Layout:
 *   ┌──────────────────────────────────────────────────────┐
 *   │  Enrichment cost estimate                   [×]      │
 *   │  ──────────────────────────────────────────────────  │
 *   │  Critical  Sonnet   110 entities  88,000 tok  $0.26  │
 *   │  High      Haiku    890 entities  445,000 tok $0.27  │
 *   │  ...                                                  │
 *   │  ──────────────────────────────────────────────────  │
 *   │  Total              5,850         2,615,000   ~$1.79 │
 *   │  Est. time: ~12 min · 380 already enriched (skipped) │
 *   │                                                       │
 *   │  [Cancel]                 [Run enrichment  ~$1.79]   │
 *   └──────────────────────────────────────────────────────┘
 *
 * Pricing constants are defined server-side in internal/enrichment/pricing.go.
 * The frontend only displays what the backend returns.
 */

import { useEffect, useRef } from 'react'
import { X, DollarSign, Clock, Sparkles, AlertTriangle } from 'lucide-react'
import type { EnrichmentEstimateResponse, EnrichmentEstimateTier } from '@/types/api'

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000)     return `${(n / 1_000).toFixed(0)}k`
  return String(n)
}

function fmtUSD(n: number): string {
  if (n === 0) return '$0.00'
  if (n < 0.01) return '<$0.01'
  return `$${n.toFixed(2)}`
}

function fmtMinutes(n: number): string {
  if (n < 1) return '<1 min'
  return `~${Math.round(n)} min`
}

const BAND_META: Record<string, { label: string; color: string; badgeColor: string }> = {
  critical: {
    label: 'Critical',
    color: 'text-red-600 dark:text-red-400',
    badgeColor: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border-red-200 dark:border-red-700',
  },
  high: {
    label: 'High',
    color: 'text-orange-600 dark:text-orange-400',
    badgeColor: 'bg-orange-100 dark:bg-orange-900/40 text-orange-700 dark:text-orange-300 border-orange-200 dark:border-orange-700',
  },
  medium: {
    label: 'Medium',
    color: 'text-yellow-600 dark:text-yellow-500',
    badgeColor: 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-700',
  },
  low: {
    label: 'Low',
    color: 'text-slate-500 dark:text-slate-400',
    badgeColor: 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 border-slate-200 dark:border-slate-700',
  },
}

const MODEL_LABEL: Record<string, string> = {
  sonnet: 'Sonnet',
  haiku:  'Haiku',
}

// ─────────────────────────────────────────────────────────────────────────────
// TierRow
// ─────────────────────────────────────────────────────────────────────────────

function TierRow({ tier }: { tier: EnrichmentEstimateTier }) {
  const meta = BAND_META[tier.band] ?? BAND_META.low
  return (
    <tr className="border-t border-slate-700/40">
      <td className="py-2 pr-4">
        <span className={`text-sm font-semibold ${meta.color}`}>{meta.label}</span>
      </td>
      <td className="py-2 pr-4">
        <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-mono font-bold border ${meta.badgeColor}`}>
          {MODEL_LABEL[tier.model] ?? tier.model}
        </span>
      </td>
      <td className="py-2 pr-4 text-right tabular-nums text-sm text-slate-300">
        {tier.count.toLocaleString()}
      </td>
      <td className="py-2 pr-4 text-right tabular-nums text-sm text-slate-400 font-mono">
        {fmtTokens(tier.est_tokens)}
      </td>
      <td className="py-2 text-right tabular-nums text-sm text-slate-200 font-semibold">
        {fmtUSD(tier.est_usd)}
      </td>
    </tr>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// EnrichmentCostModal
// ─────────────────────────────────────────────────────────────────────────────

export interface EnrichmentCostModalProps {
  /** The fetched estimate. Pass null to show a loading state. */
  estimate: EnrichmentEstimateResponse | null
  /** Whether the estimate is being fetched. */
  isLoading: boolean
  /** Whether the fetch failed. */
  isError: boolean
  /** Called when the user dismisses without confirming. */
  onClose: () => void
  /** Called when the user confirms the cost and wants to proceed. */
  onConfirm: () => void
  /** Whether the confirm action is in progress (disables the button). */
  isConfirming?: boolean
}

/**
 * EnrichmentCostModal renders the pre-run cost estimate in a modal overlay.
 * The confirm button is disabled when there are no pending candidates.
 */
export function EnrichmentCostModal({
  estimate,
  isLoading,
  isError,
  onClose,
  onConfirm,
  isConfirming = false,
}: EnrichmentCostModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null)

  // Trap focus inside the modal and close on Escape.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    dialogRef.current?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const hasData = estimate != null
  const totalPending = hasData ? estimate.tiers.reduce((s, t) => s + t.count, 0) : 0
  const canConfirm = hasData && totalPending > 0 && !isConfirming

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm"
        aria-hidden
        onClick={onClose}
        data-testid="cost-modal-backdrop"
      />

      {/* Dialog */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Enrichment cost estimate"
        tabIndex={-1}
        data-testid="enrichment-cost-modal"
        className={[
          'fixed inset-x-4 top-[10vh] z-50 mx-auto max-w-2xl',
          'flex flex-col rounded-2xl shadow-2xl outline-none',
          'bg-slate-900 border border-slate-700',
          'max-h-[85vh]',
        ].join(' ')}
      >
        {/* ── Header ──────────────────────────────────────────────────────── */}
        <div className="flex items-start justify-between px-6 pt-5 pb-4 border-b border-slate-700/60 shrink-0">
          <div className="flex items-center gap-2">
            <DollarSign className="w-5 h-5 text-sky-400 shrink-0" />
            <h2 className="text-lg font-semibold text-slate-100">
              Enrichment cost estimate
            </h2>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="p-1.5 rounded-lg text-slate-400 hover:text-slate-200 hover:bg-slate-800 transition-colors shrink-0"
            data-testid="cost-modal-close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* ── Body ────────────────────────────────────────────────────────── */}
        <div className="flex-1 overflow-y-auto px-6 py-5">
          {isLoading && (
            <div className="flex items-center justify-center py-12 text-slate-400 text-sm gap-2">
              <Sparkles className="w-4 h-4 animate-pulse" />
              Calculating estimate…
            </div>
          )}

          {isError && !isLoading && (
            <div className="flex items-center gap-2 px-4 py-3 rounded-lg bg-red-900/20 border border-red-800 text-sm text-red-400">
              <AlertTriangle className="w-4 h-4 shrink-0" />
              Failed to load cost estimate. Is the daemon running?
            </div>
          )}

          {hasData && !isLoading && (
            <>
              {/* ── Tier breakdown table ─────────────────────────────────── */}
              <div className="overflow-x-auto">
                <table
                  className="w-full text-left"
                  aria-label="Cost breakdown by tier"
                  data-testid="cost-breakdown-table"
                >
                  <thead>
                    <tr className="text-xs text-slate-500 uppercase tracking-wide">
                      <th className="pb-2 pr-4 font-medium">Tier</th>
                      <th className="pb-2 pr-4 font-medium">Model</th>
                      <th className="pb-2 pr-4 text-right font-medium">Entities</th>
                      <th className="pb-2 pr-4 text-right font-medium">Tokens</th>
                      <th className="pb-2 text-right font-medium">Est. cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {estimate.tiers
                      .filter((t) => t.count > 0)
                      .map((tier) => <TierRow key={tier.band} tier={tier} />)
                    }
                    {totalPending === 0 && (
                      <tr>
                        <td colSpan={5} className="py-6 text-center text-sm text-slate-500">
                          No pending candidates — nothing to enrich.
                        </td>
                      </tr>
                    )}
                  </tbody>
                  {totalPending > 0 && (
                    <tfoot>
                      <tr className="border-t-2 border-slate-600">
                        <td className="pt-3 pr-4 text-sm font-bold text-slate-200">Total</td>
                        <td className="pt-3 pr-4" />
                        <td className="pt-3 pr-4 text-right tabular-nums text-sm font-bold text-slate-200">
                          {totalPending.toLocaleString()}
                        </td>
                        <td className="pt-3 pr-4 text-right tabular-nums text-sm font-mono text-slate-300">
                          {fmtTokens(estimate.total_est_tokens)}
                        </td>
                        <td className="pt-3 text-right tabular-nums text-base font-bold text-sky-300">
                          {fmtUSD(estimate.total_est_usd)}
                        </td>
                      </tr>
                    </tfoot>
                  )}
                </table>
              </div>

              {/* ── Meta row ─────────────────────────────────────────────── */}
              {totalPending > 0 && (
                <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-400">
                  <span className="flex items-center gap-1">
                    <Clock className="w-3.5 h-3.5 shrink-0" />
                    Est. run time: {fmtMinutes(estimate.est_minutes)}
                  </span>
                  {estimate.already_enriched > 0 && (
                    <span>
                      {estimate.already_enriched.toLocaleString()} already enriched — skipped
                    </span>
                  )}
                </div>
              )}

              {/* ── Budget warning ───────────────────────────────────────── */}
              {estimate.total_est_usd >= 10 && (
                <div className="mt-4 flex items-start gap-2 px-4 py-3 rounded-lg bg-amber-900/20 border border-amber-700/60 text-sm text-amber-300">
                  <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
                  <span>
                    This run costs <strong>{fmtUSD(estimate.total_est_usd)}</strong> — confirm you want to proceed.
                  </span>
                </div>
              )}
            </>
          )}
        </div>

        {/* ── Footer ──────────────────────────────────────────────────────── */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-slate-700/60 shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 rounded-lg text-sm font-medium text-slate-300 hover:text-slate-100 hover:bg-slate-800 transition-colors"
            data-testid="cost-modal-cancel"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={!canConfirm}
            onClick={canConfirm ? onConfirm : undefined}
            data-testid="cost-modal-confirm"
            className={[
              'flex items-center gap-2 px-5 py-2 rounded-lg text-sm font-semibold transition-colors',
              canConfirm
                ? 'bg-sky-600 hover:bg-sky-500 text-white shadow-sm'
                : 'bg-slate-700 text-slate-500 cursor-not-allowed',
            ].join(' ')}
          >
            <Sparkles className="w-4 h-4 shrink-0" />
            {isConfirming
              ? 'Queuing…'
              : hasData && totalPending > 0
                ? `Run enrichment  ${fmtUSD(estimate.total_est_usd)}`
                : 'Run enrichment'
            }
          </button>
        </div>
      </div>
    </>
  )
}
