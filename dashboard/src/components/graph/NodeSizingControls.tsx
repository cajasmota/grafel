/**
 * NodeSizingControls — collapsible sidebar section for tunable node sizing.
 *
 * Issue #1360: expose live tuning controls so the user can dial in graph node
 * sizes without guessing.
 *
 * - Base size input (2–50 px)
 * - 6 tier multiplier inputs (one per degree-percentile band)
 * - Tier count and percentile range shown per row
 * - Reset to defaults button
 * - Values persisted via useNodeSizingConfig (localStorage)
 */
import { memo, useState, useCallback } from 'react'
import type { NodeSizingConfig } from '@/hooks/graph/useNodeSizingConfig'
import {
  TIER_COUNT,
  TIER_UPPER_PERCENTILES,
  DEFAULT_BASE_SIZE,
  DEFAULT_MULTIPLIERS,
} from '@/hooks/graph/useNodeSizingConfig'
import { ChevronDown, ChevronRight, RotateCcw } from 'lucide-react'

/** Label for each tier's percentile band */
function tierLabel(index: number): string {
  const lo = index === 0 ? 0 : TIER_UPPER_PERCENTILES[index - 1]
  const hi = TIER_UPPER_PERCENTILES[index]
  return `p${lo}–p${hi}`
}

interface NodeSizingControlsProps {
  config: NodeSizingConfig
  /** Tier node counts — optional histogram hint (length === TIER_COUNT). */
  tierCounts?: number[]
  onBaseSizeChange: (v: number) => void
  onMultiplierChange: (tierIndex: number, v: number) => void
  onReset: () => void
}

/**
 * Tiny numeric input that defers onChange until blur or Enter,
 * so the user can type freely without intermediate NaN states.
 */
const TierInput = memo(function TierInput({
  value,
  min,
  max,
  step,
  ariaLabel,
  onChange,
}: {
  value: number
  min: number
  max: number
  step: number
  ariaLabel: string
  onChange: (v: number) => void
}) {
  const [raw, setRaw] = useState<string>(String(value))
  const [focused, setFocused] = useState(false)

  // Sync external value when not focused (e.g. after reset)
  const displayValue = focused ? raw : String(value)

  const commit = useCallback(
    (s: string) => {
      const parsed = parseFloat(s)
      if (!isNaN(parsed)) onChange(parsed)
      setRaw(String(isNaN(parsed) ? value : parsed))
      setFocused(false)
    },
    [onChange, value],
  )

  return (
    <input
      type="number"
      min={min}
      max={max}
      step={step}
      value={displayValue}
      aria-label={ariaLabel}
      onChange={(e) => setRaw(e.target.value)}
      onFocus={() => { setFocused(true); setRaw(String(value)) }}
      onBlur={(e) => commit(e.target.value)}
      onKeyDown={(e) => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }}
      className={[
        'w-14 text-right text-[11px] px-1 py-0.5 rounded border',
        'bg-slate-800 text-slate-200 border-slate-600',
        'focus:outline-none focus:ring-1 focus:ring-sky-400 focus:border-sky-500',
        'transition-colors',
      ].join(' ')}
    />
  )
})

export const NodeSizingControls = memo(function NodeSizingControls({
  config,
  tierCounts,
  onBaseSizeChange,
  onMultiplierChange,
  onReset,
}: NodeSizingControlsProps) {
  const [open, setOpen] = useState(false)

  const isDefault =
    config.baseSize === DEFAULT_BASE_SIZE &&
    config.multipliers.every((m, i) => m === DEFAULT_MULTIPLIERS[i])

  return (
    <div data-testid="node-sizing-controls">
      {/* Section header — click to expand/collapse */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={[
          'flex items-center gap-1.5 w-full px-2 py-1 text-left rounded transition-colors',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
          'hover:bg-slate-200/40 dark:hover:bg-slate-800/40',
        ].join(' ')}
        aria-expanded={open}
        aria-controls="node-sizing-body"
      >
        <span className="shrink-0 text-slate-500">
          {open ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
        </span>
        <span className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold flex-1">
          Node sizing
        </span>
        {!isDefault && (
          <span
            className="w-1.5 h-1.5 rounded-full bg-sky-400 shrink-0"
            title="Custom sizing active"
            aria-label="Custom sizing active"
          />
        )}
      </button>

      {open && (
        <div
          id="node-sizing-body"
          className="px-2 pb-2 flex flex-col gap-1"
          data-testid="node-sizing-body"
        >
          {/* Base size row */}
          <div className="flex items-center justify-between gap-1 mt-1">
            <span className="text-[11px] text-slate-400 flex-1">Base (px)</span>
            <TierInput
              value={config.baseSize}
              min={2}
              max={50}
              step={1}
              ariaLabel="Base node size in pixels"
              onChange={onBaseSizeChange}
            />
          </div>

          {/* Tier multiplier rows */}
          <div className="mt-1 flex flex-col gap-0.5">
            <div className="flex items-center justify-between mb-0.5">
              <span className="text-[9px] uppercase tracking-wider text-slate-600">Tier</span>
              <span className="text-[9px] uppercase tracking-wider text-slate-600">×</span>
            </div>
            {Array.from({ length: TIER_COUNT }, (_, i) => {
              const count = tierCounts?.[i] ?? 0
              return (
                <div key={i} className="flex items-center gap-1">
                  <span
                    className="text-[10px] text-slate-500 tabular-nums flex-1 truncate"
                    title={tierLabel(i)}
                  >
                    {tierLabel(i)}
                    {tierCounts != null && (
                      <span className="ml-1 text-slate-600">({count})</span>
                    )}
                  </span>
                  <TierInput
                    value={config.multipliers[i]}
                    min={0.1}
                    max={100}
                    step={0.5}
                    ariaLabel={`Tier ${i + 1} size multiplier`}
                    onChange={(v) => onMultiplierChange(i, v)}
                  />
                </div>
              )
            })}
          </div>

          {/* Reset button */}
          <button
            type="button"
            onClick={onReset}
            disabled={isDefault}
            className={[
              'mt-1.5 flex items-center justify-center gap-1 w-full px-2 py-1 rounded text-[10px] border transition-colors',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              isDefault
                ? 'opacity-40 cursor-default border-slate-700 text-slate-500'
                : 'border-slate-600 text-slate-400 hover:border-sky-600 hover:text-sky-400',
            ].join(' ')}
            aria-label="Reset node sizing to defaults"
          >
            <RotateCcw className="w-2.5 h-2.5" />
            Reset to defaults
          </button>
        </div>
      )}
    </div>
  )
})
