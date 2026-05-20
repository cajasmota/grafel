import type { LucideIcon } from 'lucide-react'
import { SearchX } from 'lucide-react'

interface EmptyStateProps {
  icon?: LucideIcon
  title: string
  message?: string
  action?: React.ReactNode
}

export function EmptyState({ icon: Icon = SearchX, title, message, action }: EmptyStateProps) {
  return (
    <div
      className="flex flex-col items-center justify-center gap-3 py-16 text-center"
      role="status"
      aria-live="polite"
    >
      <div className="rounded-full bg-slate-200 dark:bg-slate-800 p-4">
        <Icon className="w-8 h-8 text-slate-400 dark:text-slate-500" aria-hidden />
      </div>
      <h3 className="text-base font-semibold text-slate-700 dark:text-slate-300">{title}</h3>
      {message && <p className="max-w-sm text-sm text-slate-400 dark:text-slate-500">{message}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
