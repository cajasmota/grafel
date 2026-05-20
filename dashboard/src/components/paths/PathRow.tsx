import { useNavigate } from 'react-router-dom'
import { Webhook } from 'lucide-react'
import { VerbChip } from '@/components/shared/VerbChip'
import { RepoChip } from '@/components/shared/RepoChip'
import { MultiplicityBadge } from './MultiplicityBadge'
import { splitPathParts, sortVerbs } from '@/lib/pathUtils'
import type { PathRow as PathRowType } from '@/types/api'

interface PathRowProps {
  path: PathRowType
  group: string
  isSelected?: boolean
  onSelect?: (hash: string) => void
}

export function PathRow({ path, group, isSelected = false, onSelect }: PathRowProps) {
  const navigate = useNavigate()
  const parts = splitPathParts(path.path)
  const verbs = sortVerbs(path.verbs ?? [])

  const handleClick = () => {
    onSelect?.(path.path_hash)
    navigate(`/paths/${group}/${path.path_hash}`)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      handleClick()
    }
  }

  return (
    <div
      role="row"
      tabIndex={0}
      aria-selected={isSelected}
      className={[
        'group flex items-center gap-3 px-4 py-2.5 border-b border-slate-200 dark:border-slate-800',
        'cursor-pointer hover:bg-slate-200/60 dark:hover:bg-slate-800/60 focus:outline-none focus:bg-slate-800/80',
        'transition-colors duration-75',
        isSelected ? 'bg-slate-200/80 dark:bg-slate-800/80 border-l-2 border-l-sky-500' : '',
      ].filter(Boolean).join(' ')}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
    >
      {/* Path string */}
      <span className="flex-1 min-w-0 font-mono text-sm truncate" title={path.path}>
        {parts.map((part, i) => (
          <span
            key={i}
            className={part.isDynamic ? 'text-amber-400' : 'text-slate-700 dark:text-slate-300'}
          >
            {part.text}
          </span>
        ))}
      </span>

      {/* Verb chips */}
      <span className="flex items-center gap-1 flex-shrink-0">
        {verbs.map((verb) => (
          <VerbChip key={verb} verb={verb} />
        ))}
      </span>

      {/* Multiplicity */}
      <MultiplicityBadge count={path.multiplicity} />

      {/* Webhook badge */}
      {path.is_webhook && (
        <span
          className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-violet-900/40 text-violet-300 border border-violet-700"
          title={`Webhook — ${path.webhook_provider ?? 'unknown provider'}`}
        >
          <Webhook className="w-3 h-3" aria-hidden />
          {path.webhook_provider ?? 'webhook'}
        </span>
      )}

      {/* Repo chips (only shown if multiple repos) */}
      {(path.repos ?? []).length > 0 && (
        <span className="hidden md:flex items-center gap-1 flex-shrink-0">
          {(path.repos ?? []).slice(0, 2).map((r) => (
            <RepoChip key={r} repo={r} />
          ))}
          {(path.repos ?? []).length > 2 && (
            <span className="text-xs text-slate-400 dark:text-slate-500">+{(path.repos ?? []).length - 2}</span>
          )}
        </span>
      )}
    </div>
  )
}
