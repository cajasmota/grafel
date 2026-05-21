/**
 * SimulationControls — collapsible sidebar section for tunable force-simulation params (#1361).
 *
 * Renders a slider per Cosmograph simulation knob, two preset buttons
 * (Silk Road / Dense), and a "Copy share link" button for URL-hash sharing.
 *
 * Design mirrors the "Color by" section in graph.tsx's left sidebar:
 *   - section header
 *   - collapsible via a chevron button
 *   - each control on its own row with label + value badge
 *
 * All changes are live-applied via the `setParam` / `applyPreset` callbacks
 * passed from the parent (debounce is handled at the GraphCanvas prop level —
 * Cosmograph re-reads the simulation props on next tick).
 */

import { useState, useCallback, useRef } from 'react'
import { ChevronDown, ChevronRight, Copy, Check } from 'lucide-react'
import type { SimulationConfig, SimulationPreset } from '@/hooks/graph/useSimulationConfig'
import { SLIDER_META } from '@/hooks/graph/useSimulationConfig'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface SimulationControlsProps {
  config:      SimulationConfig
  setParam:    (key: keyof SimulationConfig, value: number) => void
  applyPreset: (preset: SimulationPreset) => void
  shareHash:   string
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function SimulationControls({
  config,
  setParam,
  applyPreset,
  shareHash,
}: SimulationControlsProps) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const handleCopyShare = useCallback(() => {
    const url = window.location.origin + window.location.pathname + shareHash
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true)
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
      copyTimerRef.current = setTimeout(() => setCopied(false), 2000)
    }).catch(() => {
      // Fallback: nothing to do; clipboard API may be blocked in non-https
    })
  }, [shareHash])

  return (
    <div data-testid="simulation-controls">
      {/* Section header — clicking anywhere on it toggles collapse */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls="sim-controls-body"
        className={[
          'flex items-center justify-between w-full px-2 py-1 rounded',
          'text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold',
          'hover:bg-slate-200/40 dark:hover:bg-slate-800/40 transition-colors',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
        ].join(' ')}
        data-testid="sim-controls-toggle"
      >
        <span>Simulation</span>
        {open
          ? <ChevronDown className="w-3 h-3 text-slate-400" aria-hidden />
          : <ChevronRight className="w-3 h-3 text-slate-400" aria-hidden />
        }
      </button>

      {open && (
        <div
          id="sim-controls-body"
          className="flex flex-col gap-2 mt-1 px-1"
          data-testid="sim-controls-body"
        >
          {/* Sliders */}
          {SLIDER_META.map(({ key, label, min, max, step }) => {
            const val = config[key]
            const displayVal = Number.isInteger(step) ? String(val) : val.toFixed(2)
            return (
              <div key={key} className="flex flex-col gap-0.5">
                <div className="flex items-center justify-between px-1">
                  <label
                    htmlFor={`sim-slider-${key}`}
                    className="text-[10px] text-slate-500 dark:text-slate-500"
                  >
                    {label}
                  </label>
                  <span
                    className="text-[10px] font-mono text-slate-400 dark:text-slate-400 tabular-nums"
                    aria-live="polite"
                    aria-label={`${label} value: ${displayVal}`}
                  >
                    {displayVal}
                  </span>
                </div>
                <input
                  id={`sim-slider-${key}`}
                  type="range"
                  min={min}
                  max={max}
                  step={step}
                  value={val}
                  onChange={(e) => setParam(key, Number(e.target.value))}
                  className={[
                    'w-full h-1 rounded-full appearance-none cursor-pointer',
                    'bg-slate-300 dark:bg-slate-700',
                    'accent-sky-500',
                  ].join(' ')}
                  aria-label={label}
                  aria-valuemin={min}
                  aria-valuemax={max}
                  aria-valuenow={val}
                  data-testid={`sim-slider-${key}`}
                />
              </div>
            )
          })}

          {/* Preset buttons */}
          <div className="flex flex-col gap-1 mt-1">
            <p className="text-[9px] uppercase tracking-wider text-slate-500/70 dark:text-slate-600/70 font-semibold px-1">
              Presets
            </p>
            <div className="flex gap-1">
              <button
                type="button"
                onClick={() => applyPreset('silk-road')}
                className={[
                  'flex-1 px-1.5 py-1 rounded text-[10px] font-medium transition-colors',
                  'bg-slate-200/60 dark:bg-slate-800/60 text-slate-600 dark:text-slate-400',
                  'hover:bg-sky-900/30 hover:text-sky-300',
                  'border border-slate-300 dark:border-slate-700',
                  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                ].join(' ')}
                aria-label="Reset to Silk Road defaults"
                data-testid="sim-preset-silk-road"
              >
                Silk Road
              </button>
              <button
                type="button"
                onClick={() => applyPreset('dense')}
                className={[
                  'flex-1 px-1.5 py-1 rounded text-[10px] font-medium transition-colors',
                  'bg-slate-200/60 dark:bg-slate-800/60 text-slate-600 dark:text-slate-400',
                  'hover:bg-amber-900/30 hover:text-amber-300',
                  'border border-slate-300 dark:border-slate-700',
                  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                ].join(' ')}
                aria-label="Apply Dense preset"
                data-testid="sim-preset-dense"
              >
                Dense
              </button>
            </div>

            {/* Share link button */}
            <button
              type="button"
              onClick={handleCopyShare}
              className={[
                'flex items-center justify-center gap-1 w-full px-1.5 py-1 rounded text-[10px] font-medium transition-colors',
                'bg-slate-200/40 dark:bg-slate-800/40 text-slate-500 dark:text-slate-500',
                'hover:text-sky-400 hover:border-sky-600',
                'border border-slate-300 dark:border-slate-700',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              ].join(' ')}
              aria-label="Copy share link with current simulation config"
              data-testid="sim-share-btn"
            >
              {copied
                ? <><Check className="w-3 h-3 text-sky-400" aria-hidden /> Copied!</>
                : <><Copy className="w-3 h-3" aria-hidden /> Share config</>
              }
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
