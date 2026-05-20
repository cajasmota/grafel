import { ChevronLeft, ChevronRight } from 'lucide-react'

interface PaginationProps {
  page: number
  pageSize: number
  total: number
  hasMore: boolean
  onPageChange: (page: number) => void
}

export function Pagination({ page, pageSize, total, hasMore, onPageChange }: PaginationProps) {
  // Guard against NaN/0 from URL param parsing
  const safePage = (Number.isFinite(page) && page >= 1) ? page : 1
  const safePageSize = (Number.isFinite(pageSize) && pageSize >= 1) ? pageSize : 50
  const start = (safePage - 1) * safePageSize + 1
  const end = Math.min(safePage * safePageSize, total)
  const hasPrev = safePage > 1

  return (
    <nav
      className="flex items-center justify-between px-4 py-2 border-t border-slate-200 dark:border-slate-800"
      aria-label="Pagination"
    >
      <span className="text-xs text-slate-400 dark:text-slate-500">
        {total > 0 ? `${start}–${end} of ${total}` : 'No results'}
      </span>
      <div className="flex items-center gap-1">
        <PaginationButton
          label="Previous page"
          icon={<ChevronLeft className="w-4 h-4" />}
          disabled={!hasPrev}
          onClick={() => onPageChange(safePage - 1)}
        />
        <span className="px-2 text-xs text-slate-400 dark:text-slate-400">
          Page {safePage}
        </span>
        <PaginationButton
          label="Next page"
          icon={<ChevronRight className="w-4 h-4" />}
          disabled={!hasMore}
          onClick={() => onPageChange(safePage + 1)}
        />
      </div>
    </nav>
  )
}

interface PaginationButtonProps {
  label: string
  icon: React.ReactNode
  disabled: boolean
  onClick: () => void
}

function PaginationButton({ label, icon, disabled, onClick }: PaginationButtonProps) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      className={[
        'flex items-center justify-center w-7 h-7 rounded',
        'transition-colors',
        disabled
          ? 'text-slate-700 cursor-not-allowed'
          : 'text-slate-400 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-800 hover:text-slate-800 dark:hover:text-slate-200',
      ].join(' ')}
      onClick={onClick}
    >
      {icon}
    </button>
  )
}
