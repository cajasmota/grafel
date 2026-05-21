/**
 * TopologyList — grouped / scannable list view for Surface 3.
 *
 * Three grouping modes: 'broker' | 'By repo' | 'By protocol'.
 *   - 'broker'    — top-level: broker section headers with icon + name + count badge + health summary
 *                   sub-level: owning service/repo sub-headers
 *   - 'repo'      — group by source repo (original behaviour)
 *   - 'protocol'  — group by broker/channel protocol (original behaviour)
 *
 * Broker-level sections (#1142):
 *   - Collapsible via ChevronDown/ChevronRight; state persisted to localStorage keyed by
 *     `archigraph:topology-group-{broker_slug}`
 *   - Each section shows: icon, broker name, entity count, health summary pill
 *   - Within each broker section, service sub-headers group entries by owning repo
 *   - Sort control per-section: name | producers | consumers
 *   - Cross-repo flag chip when a row's producer repo differs from its consumer repo
 */

import { useState, useMemo, useCallback, useEffect } from 'react'
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
  ServerCrash,
  AlertCircle,
  ArrowUpDown,
  GitFork,
} from 'lucide-react'
import type { TopologyResponse, TopologyProtocol } from '@/types/api'
import { PROTOCOL_COLORS } from '@/lib/colors'
import { RepoChip } from '@/components/shared/RepoChip'
import {
  flattenTopologyEntities,
  groupTopologyEntities,
  groupTopologyByBroker,
  type TopologyListRow,
  type TopologyGrouping,
  type BrokerGroup,
  type BrokerSortField,
} from '@/lib/groupTopologyEntities'

// ────────────────────────────────────────────────────────────────────────────
// localStorage persistence
// ────────────────────────────────────────────────────────────────────────────

const LS_GROUPING_KEY = 'archigraph:topology-list-grouping'

// Extended grouping type now includes 'broker' mode
type ExtendedGrouping = TopologyGrouping | 'broker'

function readGroupingPref(): ExtendedGrouping {
  try {
    const v = localStorage.getItem(LS_GROUPING_KEY)
    if (v === 'repo' || v === 'protocol' || v === 'broker') return v
  } catch { /* noop */ }
  return 'broker'
}

function writeGroupingPref(v: ExtendedGrouping) {
  try { localStorage.setItem(LS_GROUPING_KEY, v) } catch { /* noop */ }
}

// Per-broker collapsed/expanded state — keyed by `archigraph:topology-group-{slug}`
const LS_BROKER_GROUP_PREFIX = 'archigraph:topology-group-'

function readBrokerExpanded(slug: string): boolean {
  try {
    const v = localStorage.getItem(`${LS_BROKER_GROUP_PREFIX}${slug}`)
    if (v === 'false') return false
  } catch { /* noop */ }
  return true // default: expanded
}

function writeBrokerExpanded(slug: string, expanded: boolean) {
  try { localStorage.setItem(`${LS_BROKER_GROUP_PREFIX}${slug}`, String(expanded)) } catch { /* noop */ }
}

// ────────────────────────────────────────────────────────────────────────────
// Protocol icon — lucide-react icon per protocol
// ────────────────────────────────────────────────────────────────────────────

function ProtocolIcon({ protocol, className = '' }: { protocol: TopologyProtocol; className?: string }) {
  const props = { className: `w-3.5 h-3.5 ${className}`, 'aria-hidden': true as const }
  switch (protocol) {
    case 'kafka':                return <Layers {...props} />
    case 'rabbitmq':             return <Share2 {...props} />
    case 'sqs':                  return <GitBranch {...props} />
    case 'pubsub':               return <Radio {...props} />
    case 'nats':                 return <Zap {...props} />
    case 'websocket':            return <Rss {...props} />
    case 'sse':                  return <Rss {...props} />
    case 'graphql_subscription': return <Share2 {...props} />
    case 'redis':                return <ServerCrash {...props} />
    case 'redis-stream':         return <ServerCrash {...props} />
    case 'redis_pubsub':         return <ServerCrash {...props} />
    case 'task-queue':           return <ArrowUpDown {...props} />
    case 'serverless':           return <Zap {...props} />
    default:                     return <Layers {...props} />
  }
}

// ────────────────────────────────────────────────────────────────────────────
// Single row
// ────────────────────────────────────────────────────────────────────────────

interface TopologyListRowProps {
  row: TopologyListRow
  isSelected: boolean
  onSelect: (id: string) => void
  /** When true, show a cross-repo chip (producer and consumer in different repos) */
  showCrossRepoChip?: boolean
}

function TopologyListRowItem({ row, isSelected, onSelect, showCrossRepoChip }: TopologyListRowProps) {
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
      <span className={`flex-shrink-0 ${spec?.text ?? 'text-slate-400'}`}>
        <ProtocolIcon protocol={row.protocol} />
      </span>

      {/* Label */}
      <span
        className="font-mono text-xs text-slate-800 dark:text-slate-200 flex-1 truncate min-w-0"
        title={row.label}
      >
        {row.label}
      </span>

      {/* Cross-repo flag chip */}
      {showCrossRepoChip && (
        <span
          title="Producer and consumer are in different repos"
          className="flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded-full bg-amber-900/40 text-amber-300 border border-amber-700 flex-shrink-0"
          aria-label="Cross-repo"
        >
          <GitFork className="w-2.5 h-2.5" aria-hidden />
          cross-repo
        </span>
      )}

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
// Health summary pill
// ────────────────────────────────────────────────────────────────────────────

interface HealthSummaryProps {
  brokerLabel: string
  health: BrokerGroup['health']
}

function HealthSummary({ brokerLabel, health }: HealthSummaryProps) {
  const parts: string[] = []
  if (health.active > 0) parts.push(`${health.active} active`)
  if (health.orphanPublishers > 0) parts.push(`${health.orphanPublishers} orphan pub`)
  if (health.orphanSubscribers > 0) parts.push(`${health.orphanSubscribers} orphan sub`)

  const hasIssues = health.orphanPublishers > 0 || health.orphanSubscribers > 0

  return (
    <span
      title={`${brokerLabel}: ${health.active} active, ${health.orphanPublishers} orphan publishers, ${health.orphanSubscribers} orphan subscribers`}
      className={[
        'flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded-full flex-shrink-0',
        hasIssues
          ? 'bg-amber-900/30 text-amber-400 border border-amber-800'
          : 'bg-emerald-900/30 text-emerald-400 border border-emerald-800',
      ].join(' ')}
    >
      {hasIssues && <AlertCircle className="w-2.5 h-2.5 flex-shrink-0" aria-hidden />}
      {parts.join(', ')}
    </span>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Service sub-header + rows
// ────────────────────────────────────────────────────────────────────────────

interface ServiceSectionProps {
  service: string
  rows: TopologyListRow[]
  selectedId: string | null
  onSelect: (id: string) => void
  showCrossRepoChips?: boolean
}

function ServiceSection({ service, rows, selectedId, onSelect, showCrossRepoChips }: ServiceSectionProps) {
  // Only render a sub-header when there's >1 service in the broker group — caller decides
  return (
    <>
      <div className="flex items-center gap-2 px-5 py-1 bg-slate-50/30 dark:bg-slate-950/30 border-b border-slate-200/50 dark:border-slate-800/50">
        <span className="text-[10px] font-semibold text-slate-500 dark:text-slate-500 uppercase tracking-wide truncate">
          {service}
        </span>
        <span className="text-[10px] text-slate-400 dark:text-slate-600 tabular-nums ml-auto flex-shrink-0">
          {rows.length}
        </span>
      </div>
      {rows.map((row) => (
        <TopologyListRowItem
          key={row.id}
          row={row}
          isSelected={row.id === selectedId}
          onSelect={onSelect}
          showCrossRepoChip={showCrossRepoChips && row.producerCount > 0 && row.consumerCount > 0}
        />
      ))}
    </>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Broker section
// ────────────────────────────────────────────────────────────────────────────

interface BrokerSectionProps {
  group: BrokerGroup
  isExpanded: boolean
  onToggle: () => void
  sortBy: BrokerSortField
  onSortChange: (s: BrokerSortField) => void
  selectedId: string | null
  onSelect: (id: string) => void
  searchQuery: string
}

function BrokerSection({
  group,
  isExpanded,
  onToggle,
  sortBy,
  onSortChange,
  selectedId,
  onSelect,
  searchQuery,
}: BrokerSectionProps) {
  const spec = PROTOCOL_COLORS[group.slug]

  // When searching, always show all rows regardless of collapse state
  const effectivelyExpanded = isExpanded || !!searchQuery

  // Determine if service sub-headers are worth showing (>1 service)
  const showServices = group.services.length > 1

  return (
    <div data-broker={group.slug} className="border-b border-slate-200 dark:border-slate-800 last:border-b-0">
      {/* Broker header */}
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={effectivelyExpanded}
        aria-label={`${group.label} broker section, ${group.rows.length} topics`}
        className={[
          'w-full flex items-center gap-2 px-3 py-2 transition-colors focus:outline-none',
          'bg-slate-100/90 dark:bg-slate-900/90 hover:bg-slate-200 dark:hover:bg-slate-900',
          'border-b border-slate-200 dark:border-slate-800',
        ].join(' ')}
      >
        {effectivelyExpanded
          ? <ChevronDown className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" aria-hidden />
          : <ChevronRight className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" aria-hidden />
        }

        {/* Broker icon */}
        <span className={`flex-shrink-0 ${spec?.text ?? 'text-slate-400'}`}>
          <ProtocolIcon protocol={group.slug} />
        </span>

        {/* Broker name */}
        <span className="text-xs font-semibold text-slate-700 dark:text-slate-200 flex-1 truncate text-left">
          {group.label}
        </span>

        {/* Health summary */}
        <HealthSummary brokerLabel={group.label} health={group.health} />

        {/* Count badge */}
        <span className={[
          'text-xs tabular-nums px-1.5 py-0.5 rounded-full flex-shrink-0',
          spec?.bg ?? 'bg-slate-800',
          spec?.text ?? 'text-slate-300',
        ].join(' ')}>
          {group.rows.length}
        </span>
      </button>

      {/* Expanded body */}
      {effectivelyExpanded && (
        <div role="rowgroup">
          {/* Sort controls */}
          <div className="flex items-center gap-1 px-4 py-1.5 border-b border-slate-200/50 dark:border-slate-800/50 bg-white/50 dark:bg-slate-950/50">
            <span className="text-[10px] text-slate-400 dark:text-slate-500 mr-1">Sort:</span>
            {(['name', 'producers', 'consumers'] as BrokerSortField[]).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => onSortChange(f)}
                aria-pressed={sortBy === f}
                className={[
                  'text-[10px] px-1.5 py-0.5 rounded transition-colors focus:outline-none',
                  sortBy === f
                    ? 'bg-sky-900/50 text-sky-300 border border-sky-700'
                    : 'text-slate-400 hover:text-slate-200 border border-transparent hover:border-slate-700',
                ].join(' ')}
              >
                {f === 'name' ? 'Name' : f === 'producers' ? 'Producers' : 'Consumers'}
              </button>
            ))}
          </div>

          {/* Rows — grouped by service when multiple services */}
          {group.rows.length === 0 ? (
            <p className="px-5 py-3 text-xs text-slate-500 dark:text-slate-600 italic">
              No topics under this broker
            </p>
          ) : showServices ? (
            group.services.map((svc) => (
              <ServiceSection
                key={svc.service}
                service={svc.service}
                rows={svc.rows}
                selectedId={selectedId}
                onSelect={onSelect}
                showCrossRepoChips
              />
            ))
          ) : (
            <div className="divide-y divide-slate-800/40">
              {group.rows.map((row) => (
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
      )}
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Generic group section (repo / protocol mode — original behaviour)
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
  const [grouping, setGroupingRaw] = useState<ExtendedGrouping>(readGroupingPref)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  // Per-broker collapsed state — initialised lazily from localStorage
  const [brokerExpanded, setBrokerExpanded] = useState<Record<string, boolean>>({})
  // Per-broker sort field
  const [brokerSort, setBrokerSort] = useState<Record<string, BrokerSortField>>({})

  const setGrouping = useCallback((next: ExtendedGrouping) => {
    setGroupingRaw(next)
    writeGroupingPref(next)
    setExpandedGroups({})
  }, [])

  // Flatten + filter rows
  const rows = useMemo(
    () => flattenTopologyEntities(data, searchQuery),
    [data, searchQuery],
  )

  // Broker groups
  const brokerGroups = useMemo(
    () => groupTopologyByBroker(rows, 'name'),
    [rows],
  )

  // Initialise per-broker expanded state from localStorage when broker groups change
  useEffect(() => {
    const initial: Record<string, boolean> = {}
    for (const g of brokerGroups) {
      if (!(g.slug in brokerExpanded)) {
        initial[g.slug] = readBrokerExpanded(g.slug)
      }
    }
    if (Object.keys(initial).length > 0) {
      setBrokerExpanded((prev) => ({ ...initial, ...prev }))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [brokerGroups])

  // Legacy repo/protocol groups
  const groups = useMemo(
    () => groupTopologyEntities(rows, grouping === 'broker' ? 'repo' : grouping),
    [rows, grouping],
  )

  const toggleGroup = useCallback((name: string) => {
    setExpandedGroups((prev) => ({ ...prev, [name]: !prev[name] }))
  }, [])

  const toggleBrokerGroup = useCallback((slug: string) => {
    setBrokerExpanded((prev) => {
      const next = !prev[slug]
      writeBrokerExpanded(slug, next)
      return { ...prev, [slug]: next }
    })
  }, [])

  const setBrokerSortField = useCallback((slug: string, field: BrokerSortField) => {
    setBrokerSort((prev) => ({ ...prev, [slug]: field }))
  }, [])

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Grouping selector */}
      <div className="flex items-center gap-1 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-white/90 dark:bg-slate-950/90 flex-shrink-0">
        <span className="text-xs text-slate-400 dark:text-slate-500 mr-1">Group:</span>
        {(['broker', 'repo', 'protocol'] as ExtendedGrouping[]).map((mode) => (
          <button
            key={mode}
            type="button"
            onClick={() => setGrouping(mode)}
            aria-pressed={grouping === mode}
            className={[
              'text-xs px-2 py-0.5 rounded transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500 capitalize',
              grouping === mode
                ? 'bg-sky-900/50 text-sky-300 border border-sky-700'
                : 'text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 border border-transparent hover:border-slate-300 dark:hover:border-slate-700',
            ].join(' ')}
          >
            {mode === 'broker' ? 'By broker' : mode === 'repo' ? 'By repo' : 'By protocol'}
          </button>
        ))}
        <span className="flex-1" />
        <span className="text-xs text-slate-500 dark:text-slate-600 tabular-nums">
          {rows.length} {rows.length === 1 ? 'entity' : 'entities'}
        </span>
      </div>

      {/* Content */}
      {rows.length === 0 ? (
        <div className="flex-1 flex items-center justify-center">
          <p className="text-sm text-slate-500 dark:text-slate-600 italic">
            {searchQuery ? 'No entities match the filter.' : 'No entities to display.'}
          </p>
        </div>
      ) : grouping === 'broker' ? (
        /* ── Broker-grouped view (#1142) ─────────────────────────────────── */
        <div
          className="flex-1 overflow-y-auto"
          role="grid"
          aria-label="Topology entities grouped by broker"
          data-testid="topology-broker-groups"
        >
          {brokerGroups.map((group) => (
            <BrokerSection
              key={group.slug}
              group={{
                ...group,
                // Apply per-broker sort
                rows: [...group.rows].sort((a, b) => {
                  const s = brokerSort[group.slug] ?? 'name'
                  if (s === 'producers') return b.producerCount - a.producerCount
                  if (s === 'consumers') return b.consumerCount - a.consumerCount
                  return a.label.localeCompare(b.label)
                }),
                services: group.services.map((svc) => ({
                  ...svc,
                  rows: [...svc.rows].sort((a, b) => {
                    const s = brokerSort[group.slug] ?? 'name'
                    if (s === 'producers') return b.producerCount - a.producerCount
                    if (s === 'consumers') return b.consumerCount - a.consumerCount
                    return a.label.localeCompare(b.label)
                  }),
                })),
              }}
              isExpanded={brokerExpanded[group.slug] ?? true}
              onToggle={() => toggleBrokerGroup(group.slug)}
              sortBy={brokerSort[group.slug] ?? 'name'}
              onSortChange={(f) => setBrokerSortField(group.slug, f)}
              selectedId={selectedId}
              onSelect={onSelectEntity}
              searchQuery={searchQuery}
            />
          ))}
        </div>
      ) : (
        /* ── Legacy repo / protocol grouping ────────────────────────────── */
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
