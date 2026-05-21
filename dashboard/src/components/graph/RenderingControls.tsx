/**
 * RenderingControls — collapsible sidebar section for live render tuning.
 *
 * Mirrors the SimulationControls / NodeSizingControls UX:
 *   - Section header with chevron (click to collapse/expand)
 *   - Slider rows with live numeric value badge
 *   - Boolean toggles for scalePointsOnZoom + showLinks
 *   - "Reset to defaults" button (disabled when nothing changed)
 *   - Active-config indicator dot in header
 *
 * All changes propagate immediately via the setParam callback — debounce is
 * handled by the parent (GraphCanvas useEffect).
 */
import { useState } from 'react'
import { ChevronDown, ChevronRight, RotateCcw } from 'lucide-react'
import type { RenderConfig } from '@/hooks/graph/useRenderConfig'

// ---------------------------------------------------------------------------
// Slider metadata
// ---------------------------------------------------------------------------

interface SliderRow {
  key:   keyof RenderConfig
  label: string
  min:   number
  max:   number
  step:  number
}

const SLIDERS: SliderRow[] = [
  { key: 'pointOpacity',   label: 'Point opacity',    min: 0.05, max: 1.0,  step: 0.01 },
  { key: 'pointSizeScale', label: 'Point size scale', min: 0.05, max: 2.0,  step: 0.01 },
  { key: 'maxPointSize',   label: 'Max point size',   min: 4,    max: 200,  step: 1    },
  { key: 'linkOpacity',    label: 'Link opacity',     min: 0,    max: 1.0,  step: 0.01 },
  { key: 'linkWidthScale', label: 'Link width scale', min: 0.05, max: 2.0,  step: 0.01 },
]

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface RenderingControlsProps {
  config:       RenderConfig
  isModified:   boolean
  setParam:     <K extends keyof RenderConfig>(key: K, value: RenderConfig[K]) => void
  onReset:      () => void
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function RenderingControls({
  config,
  isModified,
  setParam,
  onReset,
}: RenderingControlsProps) {
  const [open, setOpen] = useState(false)

  return (
    <div data-testid="rendering-controls">
      {/* Section header */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls="render-controls-body"
        className={[
          'flex items-center justify-between w-full px-2 py-1 rounded',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
          'hover:bg-slate-200/40 dark:hover:bg-slate-800/40 transition-colors',
        ].join(' ')}
        data-testid="rendering-controls-toggle"
      >
        <span className="flex items-center gap-1.5">
          <span className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold">
            Rendering
          </span>
          {isModified && (
            <span
              className="w-1.5 h-1.5 rounded-full bg-sky-400 shrink-0"
              title="Custom rendering active"
              aria-label="Custom rendering active"
            />
          )}
        </span>
        {open
          ? <ChevronDown className="w-3 h-3 text-slate-400" aria-hidden />
          : <ChevronRight className="w-3 h-3 text-slate-400" aria-hidden />
        }
      </button>

      {open && (
        <div
          id="render-controls-body"
          className="flex flex-col gap-2 mt-1 px-1"
          data-testid="render-controls-body"
        >
          {/* Sliders */}
          {SLIDERS.map(({ key, label, min, max, step }) => {
            const val = config[key] as number
            const displayVal = Number.isInteger(step) ? String(val) : val.toFixed(2)
            return (
              <div key={key} className="flex flex-col gap-0.5">
                <div className="flex items-center justify-between px-1">
                  <label
                    htmlFor={`render-slider-${key}`}
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
                  id={`render-slider-${key}`}
                  type="range"
                  min={min}
                  max={max}
                  step={step}
                  value={val}
                  onChange={(e) => setParam(key, Number(e.target.value) as RenderConfig[typeof key])}
                  className={[
                    'w-full h-1 rounded-full appearance-none cursor-pointer',
                    'bg-slate-300 dark:bg-slate-700',
                    'accent-sky-500',
                  ].join(' ')}
                  aria-label={label}
                  aria-valuemin={min}
                  aria-valuemax={max}
                  aria-valuenow={val}
                  data-testid={`render-slider-${key}`}
                />
              </div>
            )
          })}

          {/* Boolean toggles */}
          <div className="flex flex-col gap-1 mt-0.5">
            {/* Scale points on zoom */}
            <div className="flex items-center justify-between px-1">
              <span className="text-[10px] text-slate-500 dark:text-slate-500">
                Scale on zoom
              </span>
              <button
                type="button"
                role="switch"
                aria-checked={config.scalePointsOnZoom}
                onClick={() => setParam('scalePointsOnZoom', !config.scalePointsOnZoom)}
                data-testid="render-toggle-scalePointsOnZoom"
                className={[
                  'relative inline-flex h-4 w-7 items-center rounded-full transition-colors',
                  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                  config.scalePointsOnZoom
                    ? 'bg-sky-500'
                    : 'bg-slate-600',
                ].join(' ')}
                aria-label="Toggle scale points on zoom"
              >
                <span
                  className={[
                    'inline-block h-3 w-3 rounded-full bg-white shadow transition-transform',
                    config.scalePointsOnZoom ? 'translate-x-3.5' : 'translate-x-0.5',
                  ].join(' ')}
                />
              </button>
            </div>

            {/* Show links */}
            <div className="flex items-center justify-between px-1">
              <span className="text-[10px] text-slate-500 dark:text-slate-500">
                Show links
              </span>
              <button
                type="button"
                role="switch"
                aria-checked={config.showLinks}
                onClick={() => setParam('showLinks', !config.showLinks)}
                data-testid="render-toggle-showLinks"
                className={[
                  'relative inline-flex h-4 w-7 items-center rounded-full transition-colors',
                  'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
                  config.showLinks
                    ? 'bg-sky-500'
                    : 'bg-slate-600',
                ].join(' ')}
                aria-label="Toggle show links"
              >
                <span
                  className={[
                    'inline-block h-3 w-3 rounded-full bg-white shadow transition-transform',
                    config.showLinks ? 'translate-x-3.5' : 'translate-x-0.5',
                  ].join(' ')}
                />
              </button>
            </div>
          </div>

          {/* Reset button */}
          <button
            type="button"
            onClick={onReset}
            disabled={!isModified}
            className={[
              'mt-0.5 flex items-center justify-center gap-1 w-full px-2 py-1 rounded text-[10px] border transition-colors',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              !isModified
                ? 'opacity-40 cursor-default border-slate-700 text-slate-500'
                : 'border-slate-600 text-slate-400 hover:border-sky-600 hover:text-sky-400',
            ].join(' ')}
            aria-label="Reset rendering to defaults"
            data-testid="render-reset-btn"
          >
            <RotateCcw className="w-2.5 h-2.5" />
            Reset to defaults
          </button>
        </div>
      )}
    </div>
  )
}
