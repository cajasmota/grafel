import type { LodLevel } from '@/types/api'

interface LodLevelIndicatorProps {
  lodLevel: LodLevel
  visibleCount: number
  totalCount: number
}

const LOD_LABELS: Record<LodLevel, string> = {
  'zoom-out': 'centroids',
  'mid': 'mid',
  'zoom-in': 'full',
  'blocked': 'blocked',
}

const LOD_COLORS: Record<LodLevel, string> = {
  'zoom-out': 'bg-slate-200/80 dark:bg-slate-800/80 text-slate-400 dark:text-slate-400 border-slate-300 dark:border-slate-700',
  'mid': 'bg-slate-200/80 dark:bg-slate-800/80 text-sky-400 border-sky-900',
  'zoom-in': 'bg-slate-200/80 dark:bg-slate-800/80 text-emerald-400 border-emerald-900',
  'blocked': 'bg-red-950/80 text-red-400 border-red-900',
}

/**
 * Subtle top-right pill showing the current LoD tier and node counts.
 */
export function LodLevelIndicator({ lodLevel, visibleCount, totalCount }: LodLevelIndicatorProps) {
  return (
    <div
      className={[
        'inline-flex items-center gap-1.5 px-2 py-1 rounded-full border text-[11px] font-mono select-none',
        LOD_COLORS[lodLevel],
      ].join(' ')}
      aria-label={`Level of detail: ${LOD_LABELS[lodLevel]}`}
      title={`${visibleCount} of ${totalCount} nodes visible`}
      role="status"
    >
      <span className="font-semibold tracking-wider uppercase">{LOD_LABELS[lodLevel]}</span>
      <span className="opacity-60">
        {visibleCount.toLocaleString()}
        {totalCount > 0 && ` / ${totalCount.toLocaleString()}`}
      </span>
    </div>
  )
}
