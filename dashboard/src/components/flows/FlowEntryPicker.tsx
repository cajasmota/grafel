import { useRef } from 'react'
import { Search, Clock, Filter } from 'lucide-react'
import { KindIcon } from '@/components/shared/KindIcon'
import { RepoChip } from '@/components/shared/RepoChip'
import type { Process } from '@/types/api'

interface FlowEntryPickerProps {
  searchQuery: string
  onSearchChange: (q: string) => void
  entryPoints: Process[]
  recent: Process[]
  isLoading: boolean
  crossStackOnly: boolean
  onCrossStackChange: (v: boolean) => void
  onSelectEntry: (process: Process) => void
  selectedEntryId?: string | null
}

export function FlowEntryPicker({
  searchQuery,
  onSearchChange,
  entryPoints,
  recent,
  isLoading,
  crossStackOnly,
  onCrossStackChange,
  onSelectEntry,
  selectedEntryId,
}: FlowEntryPickerProps) {
  const inputRef = useRef<HTMLInputElement>(null)

  const showRecent = !searchQuery && recent.length > 0

  return (
    <div className="flex flex-col gap-3 p-4 border-b border-slate-200 dark:border-slate-800">
      {/* Search input */}
      <div className="relative">
        <Search
          className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500 pointer-events-none"
          aria-hidden
        />
        <input
          ref={inputRef}
          type="text"
          role="searchbox"
          aria-label="Search entry points"
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder="Search entry points…"
          className={[
            'w-full pl-9 pr-4 py-2 rounded-lg text-sm',
            'bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 text-slate-800 dark:text-slate-200 placeholder-slate-500',
            'focus:outline-none focus:border-sky-600 focus:ring-1 focus:ring-sky-600/50',
            'transition-colors',
          ].join(' ')}
        />
        {isLoading && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 rounded-full border-2 border-sky-500 border-t-transparent animate-spin" aria-label="Loading" />
        )}
      </div>

      {/* Cross-stack filter */}
      <label className="flex items-center gap-2 text-xs text-slate-400 dark:text-slate-400 cursor-pointer select-none">
        <input
          type="checkbox"
          checked={crossStackOnly}
          onChange={(e) => onCrossStackChange(e.target.checked)}
          className="rounded border-slate-300 dark:border-slate-700 bg-slate-100 dark:bg-slate-900 text-sky-500 focus:ring-sky-500"
          aria-label="Show cross-stack flows only"
        />
        <Filter className="w-3 h-3" aria-hidden />
        Cross-stack only
      </label>

      {/* Recent section */}
      {showRecent && (
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-600 mb-1.5 flex items-center gap-1">
            <Clock className="w-3 h-3" aria-hidden />
            Recent
          </p>
          <div className="space-y-0.5">
            {recent.map((p) => (
              <EntryPointItem
                key={p.entry_id}
                process={p}
                isSelected={p.entry_id === selectedEntryId}
                onSelect={onSelectEntry}
              />
            ))}
          </div>
        </div>
      )}

      {/* Search results */}
      {searchQuery && (
        <div>
          {entryPoints.length === 0 ? (
            <p className="text-xs text-slate-400 dark:text-slate-500 py-2">No entry points match "{searchQuery}"</p>
          ) : (
            <div className="space-y-0.5 max-h-56 overflow-y-auto">
              {entryPoints.map((p) => (
                <EntryPointItem
                  key={p.entry_id}
                  process={p}
                  isSelected={p.entry_id === selectedEntryId}
                  onSelect={onSelectEntry}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function EntryPointItem({
  process,
  isSelected,
  onSelect,
}: {
  process: Process
  isSelected: boolean
  onSelect: (p: Process) => void
}) {
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect(process)
    }
  }

  return (
    <div
      role="option"
      aria-selected={isSelected}
      tabIndex={0}
      className={[
        'flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
        'transition-colors duration-75',
        isSelected
          ? 'bg-sky-900/40 text-sky-200'
          : 'hover:bg-slate-200/60 dark:hover:bg-slate-800/60 text-slate-700 dark:text-slate-300',
      ].join(' ')}
      onClick={() => onSelect(process)}
      onKeyDown={handleKeyDown}
    >
      <span className="text-slate-400 dark:text-slate-500 flex-shrink-0" aria-hidden>
        <KindIcon kind={process.entry_kind ?? 'Function'} className="w-3.5 h-3.5" />
      </span>
      <span className="flex-1 min-w-0 font-mono text-xs truncate" title={process.entry_name}>
        {process.entry_name}
      </span>
      <RepoChip repo={process.repo} className="flex-shrink-0" />
    </div>
  )
}
