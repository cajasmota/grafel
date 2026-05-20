function Skeleton({ className = '', style }: { className?: string; style?: React.CSSProperties }) {
  return <div className={`animate-pulse rounded bg-slate-200 dark:bg-slate-800 ${className}`} style={style} aria-hidden />
}

/**
 * Skeleton loader for the topology canvas and channel track.
 */
export function TopologyLoadingState() {
  return (
    <div role="status" aria-label="Loading topology…" className="flex flex-col h-full">
      {/* Filter chips skeleton */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800">
        {Array.from({ length: 9 }, (_, i) => (
          <Skeleton key={i} className={`h-6 rounded-full ${i === 0 ? 'w-8' : 'w-16'}`} />
        ))}
      </div>

      {/* Canvas skeleton — scattered circles representing topic nodes */}
      <div className="flex-1 relative bg-white dark:bg-slate-950 overflow-hidden">
        {/* Simulated scattered nodes */}
        {[
          { top: '20%', left: '25%', size: 48 },
          { top: '45%', left: '55%', size: 56 },
          { top: '30%', left: '70%', size: 40 },
          { top: '65%', left: '35%', size: 44 },
          { top: '15%', left: '50%', size: 36 },
          { top: '70%', left: '65%', size: 40 },
          { top: '50%', left: '15%', size: 48 },
          { top: '80%', left: '80%', size: 32 },
        ].map(({ top, left, size }, i) => (
          <div
            key={i}
            className="absolute"
            style={{ top, left, transform: 'translate(-50%, -50%)' }}
          >
            <Skeleton
              className="rounded-full"
              style={{ width: size, height: size } as React.CSSProperties}
            />
          </div>
        ))}
        <span className="sr-only">Loading…</span>
      </div>

      {/* Channel track skeleton */}
      <div className="border-t border-slate-200 dark:border-slate-800">
        <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800/60">
          <Skeleton className="w-3.5 h-3.5 rounded" />
          <Skeleton className="h-3.5 w-32 rounded" />
        </div>
        <div className="flex gap-2 px-4 py-2">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="w-52 h-24 rounded-lg flex-shrink-0" />
          ))}
        </div>
      </div>
    </div>
  )
}
