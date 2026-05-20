/**
 * Skeleton loaders — not spinners.
 * Each variant matches the layout of its consuming surface.
 */

interface SkeletonProps {
  className?: string
}

function Skeleton({ className = '' }: SkeletonProps) {
  return (
    <div
      className={`animate-pulse rounded bg-slate-800 ${className}`}
      aria-hidden
    />
  )
}

/** Skeleton for a single PathRow */
export function PathRowSkeleton() {
  return (
    <div className="flex items-center gap-3 px-4 py-3 border-b border-slate-800">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-5 w-10 rounded" />
      <Skeleton className="h-5 w-10 rounded" />
      <div className="ml-auto">
        <Skeleton className="h-4 w-16" />
      </div>
    </div>
  )
}

/** List of PathRow skeletons */
export function PathListSkeleton({ count = 8 }: { count?: number }) {
  return (
    <div role="status" aria-label="Loading paths…">
      {Array.from({ length: count }, (_, i) => (
        <PathRowSkeleton key={i} />
      ))}
      <span className="sr-only">Loading…</span>
    </div>
  )
}

/** Sidebar tree skeleton */
export function PathTreeSkeleton() {
  return (
    <div className="p-3 space-y-2" role="status" aria-label="Loading sidebar…">
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="flex items-center gap-2">
          <Skeleton className="w-3 h-3" />
          <Skeleton className={`h-4 ${i % 3 === 0 ? 'w-20' : i % 3 === 1 ? 'w-28' : 'w-16'}`} />
        </div>
      ))}
      <span className="sr-only">Loading…</span>
    </div>
  )
}

/** Generic card skeleton */
export function CardSkeleton() {
  return (
    <div className="rounded-lg border border-slate-800 p-4 space-y-3" role="status" aria-label="Loading…">
      <Skeleton className="h-5 w-3/4" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-5/6" />
      <span className="sr-only">Loading…</span>
    </div>
  )
}

/** Skeleton for a single FlowRow */
export function FlowRowSkeleton() {
  return (
    <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-800">
      <Skeleton className="w-4 h-4 mt-0.5 rounded" />
      <div className="flex-1 space-y-2">
        <div className="flex items-center gap-2">
          <Skeleton className="h-4 w-36" />
          <Skeleton className="h-5 w-20 rounded" />
        </div>
        <Skeleton className="h-3 w-64" />
      </div>
      <Skeleton className="h-5 w-12 rounded" />
    </div>
  )
}

/** List of FlowRow skeletons */
export function FlowListSkeleton({ count = 8 }: { count?: number }) {
  return (
    <div role="status" aria-label="Loading flows…">
      {Array.from({ length: count }, (_, i) => <FlowRowSkeleton key={i} />)}
      <span className="sr-only">Loading…</span>
    </div>
  )
}

/** Skeleton chip row for topology protocol filters */
export function ProtocolChipsSkeleton() {
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800" role="status" aria-label="Loading filters…">
      {Array.from({ length: 9 }, (_, i) => (
        <Skeleton key={i} className={`h-6 rounded-full ${i === 0 ? 'w-8' : 'w-16'}`} />
      ))}
      <span className="sr-only">Loading…</span>
    </div>
  )
}

/** Skeleton for the swim-lane panel */
export function SwimLaneSkeleton() {
  return (
    <div className="flex gap-0 overflow-x-auto" role="status" aria-label="Loading flow visualization…">
      {Array.from({ length: 2 }, (_, i) => (
        <div key={i} className="min-w-[240px] border-r border-slate-800 last:border-r-0">
          <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 bg-slate-900/60">
            <Skeleton className="w-2 h-2 rounded-full" />
            <Skeleton className="h-5 w-24 rounded" />
          </div>
          <div className="p-3 space-y-2">
            {Array.from({ length: 4 }, (_, j) => <Skeleton key={j} className="h-6 w-full rounded" />)}
          </div>
        </div>
      ))}
      <span className="sr-only">Loading…</span>
    </div>
  )
}
