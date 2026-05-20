/**
 * TopologyList — grouped / scannable list view for Surface 3.
 *
 * Mirrors what PathsGroup does for Surface 4, but for broker/channel entities.
 * Two grouping modes: 'By repo' | 'By protocol' (user preference in localStorage).
 * Each row shows: protocol icon + name + producer/consumer counts + source repo.
 * Clicking a row calls onSelectEntity → opens the same TopicDetailPanel as the map.
 */

import { useState, useMemo, useCallback } from 'react'
import {
  ArrowUp,
  ArrowDown,
  ChevronRight,
  ChevronDown,
  Layers,
  GitBranch,
  Zap,
  Radio,
  Rss,
  Share2,
} from 'lucide-react'
import type { TopologyResponse, TopologyProtocol } from '@/types/api'
import { PROTOCOL_COLORS } from '@/lib/colors'
import { RepoChip } from '@/components/shared/RepoChip'
import {
  flattenTopologyEntities,
  groupTopologyEntities,
  type TopologyListRow,
  type TopologyGrouping,
} from '@/lib/groupTopologyEntities'

// ────────────────────────────────────────────────────────────────────────────
// localStorage persistence
// ────────────────────────────────────────────────────────────────────────────

const LS_GROUPING_KEY = 'archigraph:topology-list-grouping'

function readGroupingPref(): TopologyGrouping {
  try {
    const v = localStorage.getItem(LS_GROUPING_KEY)
    if (v === 'repo' || v === 'protocol') return v
  } catch { /* noop */ }
  return 'repo'
}

function writeGroupingPref(v: TopologyGrouping) {
  try { localStorage.setItem(LS_GROUPING_KEY, v) } catch { /* noop */ }
}

// ────────────────────────────────────────────────────────────────────────────
// Protocol icon — lucide-react icon per protocol
// ────────────────────────────────────────────────────────────────────────────

function ProtocolIcon({ protocol, className = '' }: { protocol: TopologyProtocol; className?: string }) {
  const props = { className: `w-3.5 h-3.5 ${className}`, 'aria-hidden': true as const }
  switch (protocol) {
    case 'kafka':       return <Layers {...props} />
    case 'rabbitmq':    return <Share2 {...props} />
    case 'sqs':         return <GitBranch {...props} />
    case 'pubsub':      return <Radio {...props} />
    case 'nats':        return <Zap {...props} />
    case 'websocket':   return <Rss {...props} />
    case 'sse':         return <Rss {...props} />
    case 'graphql_subscription': return <Share2 {...props} />
    default:            return <Layers {...props} />
  }
}

// ────────────────────────────────────────────────────────────────────────────
// Single row
// ────────────────────────────────────────────────────────────────────────────

interface TopologyListRowProps {
  row: TopologyListRow
  isSelected: boolean
  onSelect: (id: string) => void
}

function TopologyListRowItem({ row, isSelected, onSelect }: TopologyListRowProps) {
  const spec = PROTOCOL_COLORS[row.protocol]
  return (
    <button
      type="button"
      role="row"
      aria-selected={isSelected}
      tabIndex={0}
      onClick={() => onSelect(row.id)}
      className={[
        'w-full flex items-center gap-2.5 px-4 py-2.5 text-left transition-colors group',
        'focus:outline-none focus:bg-slate-200 dark:focus:bg-slate-800',
        isSelected
          ? 'bg-slate-200 dark:bg-slate-800 border-l-2 border-sky-500'
          : 'hover:bg-slate-200/60 dark:hover:bg-slate-900/60 border-l-2 border-transparent',
      ].join(' ')}
    >
      {/* Protocol icon */}
      <span className={`flex-shrink-0 ${spec.text}`}>
        <ProtocolIcon protocol={row.protocol} />
      </span>

      {/* Label */}
      <span
        className="font-mono text-xs text-slate-800 dark:text-slate-200 flex-1 truncate min-w-0"
        title={row.label}
      >
        {row.label}
      </span>

      {/* Producer count */}
      <span
        className="flex items-center gap-0.5 text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 tabular-nums"
        title={`${row.producerCount} producer${row.producerCount !== 1 ? 's' : ''}`}
      >
        <ArrowUp className="w-3 h-3 text-slate-500 dark:text-slate-600" aria-hidden />
        {row.producerCount}
      </span>

      {/* Consumer count */}
      <span
        className="flex items-center gap-0.5 text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 tabular-nums"
        title={`${row.consumerCount} consumer${row.consumerCount !== 1 ? 's' : ''}`}
      >
        <ArrowDown className="w-3 h-3 text-slate-500 dark:text-slate-600" aria-hidden />
        {row.consumerCount}
      </span>

      {/* Source repo */}
      <RepoChip repo={row.repo} className="flex-shrink-0" />
    </button>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Group header + collapsible body
// ────────────────────────────────────────────────────────────────────────────

interface GroupSectionProps {
  name: string
  rows: TopologyListRow[]
  isExpanded: boolean
  onToggle: () => void
  selectedId: string | null
  onSelect: (id: string) => void
}

function GroupSection({ name, rows, isExpanded, onToggle, selectedId, onSelect }: GroupSectionProps) {
  return (
    <div>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={isExpanded}
        className="w-full flex items-center gap-2 px-4 py-2 bg-slate-100/80 dark:bg-slate-900/80 border-b border-slate-200 dark:border-slate-800 hover:bg-slate-200 dark:hover:bg-slate-900 transition-colors focus:outline-none focus:bg-slate-900 group"
      >
        {isExpanded
          ? <ChevronDown className="w-3.5 h-3.5 text-slate-400 dark:text-slate-500 flex-shrink-0" aria-hidden />
          : <ChevronRight className="w-3.5 h-3.5 text-slate-400 dark:text-slate-500 flex-shrink-0" aria-hidden />
        }
        <span className="text-xs font-semibold text-slate-700 dark:text-slate-300 flex-1 truncate text-left">
          {name}
        </span>
        <span className="text-xs text-slate-500 dark:text-slate-600 tabular-nums flex-shrink-0">
          {rows.length}
        </span>
      </button>

      {isExpanded && (
        <div
          role="rowgroup"
          className="divide-y divide-slate-800/40"
        >
          {rows.map((row) => (
            <TopologyListRowItem
              key={row.id}
              row={row}
              isSelected={row.id === selectedId}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Main export
// ────────────────────────────────────────────────────────────────────────────

interface TopologyListProps {
  data: TopologyResponse
  searchQuery: string
  selectedId: string | null
  onSelectEntity: (id: string) => void
}

export function TopologyList({ data, searchQuery, selectedId, onSelectEntity }: TopologyListProps) {
  const [grouping, setGroupingRaw] = useState<TopologyGrouping>(readGroupingPref)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})

  const setGrouping = useCallback((next: TopologyGrouping) => {
    setGroupingRaw(next)
    writeGroupingPref(next)
    setExpandedGroups({}) // reset expand state when switching grouping
  }, [])

  const rows = useMemo(
    () => flattenTopologyEntities(data, searchQuery),
    [data, searchQuery],
  )

  const groups = useMemo(
    () => groupTopologyEntities(rows, grouping),
    [rows, grouping],
  )

  const toggleGroup = useCallback((name: string) => {
    setExpandedGroups((prev) => ({ ...prev, [name]: !prev[name] }))
  }, [])

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Grouping selector */}
      <div className="flex items-center gap-1 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-white/90 dark:bg-slate-950/90 flex-shrink-0">
        <span className="text-xs text-slate-400 dark:text-slate-500 mr-1">Group:</span>
        <button
          type="button"
          onClick={() => setGrouping('repo')}
          aria-pressed={grouping === 'repo'}
          className={[
            'text-xs px-2 py-0.5 rounded transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500',
            grouping === 'repo'
              ? 'bg-sky-900/50 text-sky-300 border border-sky-700'
              : 'text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 border border-transparent hover:border-slate-300 dark:hover:border-slate-700',
          ].join(' ')}
        >
          By repo
        </button>
        <button
          type="button"
          onClick={() => setGrouping('protocol')}
          aria-pressed={grouping === 'protocol'}
          className={[
            'text-xs px-2 py-0.5 rounded transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500',
            grouping === 'protocol'
              ? 'bg-sky-900/50 text-sky-300 border border-sky-700'
              : 'text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 border border-transparent hover:border-slate-300 dark:hover:border-slate-700',
          ].join(' ')}
        >
          By protocol
        </button>
        <span className="flex-1" />
        <span className="text-xs text-slate-500 dark:text-slate-600 tabular-nums">
          {rows.length} {rows.length === 1 ? 'entity' : 'entities'}
        </span>
      </div>

      {/* Groups */}
      {rows.length === 0 ? (
        <div className="flex-1 flex items-center justify-center">
          <p className="text-sm text-slate-500 dark:text-slate-600 italic">
            {searchQuery ? 'No entities match the filter.' : 'No entities to display.'}
          </p>
        </div>
      ) : (
        <div
          className="flex-1 overflow-y-auto divide-y divide-slate-800/40"
          role="grid"
          aria-label="Topology entities"
        >
          {groups.map((group) => (
            <GroupSection
              key={group.name}
              name={group.name}
              rows={group.rows}
              isExpanded={!!expandedGroups[group.name]}
              onToggle={() => toggleGroup(group.name)}
              selectedId={selectedId}
              onSelect={onSelectEntity}
            />
          ))}
        </div>
      )}
    </div>
  )
}
