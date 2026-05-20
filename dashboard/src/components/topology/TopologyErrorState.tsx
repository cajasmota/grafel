import { AlertCircle } from 'lucide-react'

interface TopologyErrorStateProps {
  error: Error
  onRetry: () => void
}

export function TopologyErrorState({ error, onRetry }: TopologyErrorStateProps) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center justify-center gap-3 py-16 text-center"
    >
      <div className="rounded-full bg-red-950/40 p-4">
        <AlertCircle className="w-8 h-8 text-red-400" aria-hidden />
      </div>
      <h3 className="text-base font-semibold text-slate-300">Failed to load topology</h3>
      <p className="max-w-sm text-sm text-slate-500 font-mono">{error.message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-1 px-3 py-1.5 rounded text-sm bg-slate-800 text-slate-300 hover:bg-slate-700 transition-colors focus:outline-none focus:ring-2 focus:ring-sky-500"
      >
        Retry
      </button>
    </div>
  )
}
