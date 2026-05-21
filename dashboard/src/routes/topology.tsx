import { useState, useCallback, useEffect, useMemo } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useTopologyData } from '@/hooks/topology/useTopologyData'
import { useTopicDetail } from '@/hooks/topology/useTopicDetail'
import { useProtocolFilters } from '@/hooks/topology/useProtocolFilters'
import { useTopologyLayout } from '@/hooks/topology/useTopologyLayout'
import { useTopologySearch } from '@/hooks/topology/useTopologySearch'
import { useOrphanPublishers } from '@/hooks/topology/useOrphanPublishers'
import { useOrphanSubscribers } from '@/hooks/topology/useOrphanSubscribers'
import { ProtocolFilterChips } from '@/components/topology/ProtocolFilterChips'
import { TopologyMap } from '@/components/topology/TopologyMap'
import { TopologyList } from '@/components/topology/TopologyList'
import { TopicDetailPanel } from '@/components/topology/TopicDetailPanel'
import { ChannelTrack } from '@/components/topology/ChannelTrack'
import { GraphQLSubscriptionPanel } from '@/components/topology/GraphQLSubscriptionPanel'
import { TopologyLoadingState } from '@/components/topology/TopologyLoadingState'
import { TopologyEmptyState } from '@/components/topology/TopologyEmptyState'
import { TopologyErrorState } from '@/components/topology/TopologyErrorState'
import { OrphanPublishersTab } from '@/components/topology/OrphanPublishersTab'
import { OrphanSubscribersTab } from '@/components/topology/OrphanSubscribersTab'
import { ScheduledJobsTab } from '@/components/topology/ScheduledJobsTab'
import { Search, X, Map, List } from 'lucide-react'
import type { TopologyResponse, TopologyProtocol, QueueNode } from '@/types/api'

// ── Tab IDs ───────────────────────────────────────────────────────────────────

type TopologyTab = 'all' | 'orphan-publishers' | 'orphan-subscribers' | 'scheduled'

const TAB_PARAM = 'tab'

// ── derive available protocols from actual data ───────────────────────────────
// Only show chips for protocols that have at least one entity in the response.
// This prevents phantom chips (e.g. "Kafka") for groups with no Kafka data.
function deriveAvailableProtocols(data: TopologyResponse | undefined): TopologyProtocol[] {
  if (!data) return []
  const protocols = new Set<TopologyProtocol>()
  for (const t of data.topics ?? []) protocols.add(t.broker as TopologyProtocol)
  for (const q of data.queues ?? []) {
    // Redis stream queues use a synthetic id prefix; normalise to redis-stream.
    // #1116: Task/ScheduledJob entities have empty broker + non-empty framework;
    // fall back to 'task-queue' so PROTOCOL_COLORS lookup is always defined.
    if (q.id?.startsWith('stream:redis:')) {
      protocols.add('redis-stream')
    } else if (q.id?.startsWith('task:') || q.framework || !q.broker) {
      protocols.add('task-queue')
    } else {
      protocols.add(q.broker as TopologyProtocol)
    }
  }
  for (const c of data.channels ?? []) protocols.add(c.channel_type as TopologyProtocol)
  for (const _ of data.graphql_subscriptions ?? []) protocols.add('graphql_subscription')
  for (const _ of data.nats_subjects ?? []) protocols.add('nats')
  for (const _ of data.functions ?? []) protocols.add('serverless')
  return [...protocols].sort()
}

// ── Filter queues to scheduled jobs ──────────────────────────────────────────

function deriveScheduledJobs(data: TopologyResponse | undefined): QueueNode[] {
  if (!data) return []
  return (data.queues ?? []).filter((q) => q.scheduled === true)
}

// ────────────────────────────────────────────────────────────────────────────
// localStorage persistence for view mode
// ────────────────────────────────────────────────────────────────────────────

type ViewMode = 'map' | 'list'

const LS_VIEW_MODE_KEY = 'archigraph:topology-view-mode'

function readViewMode(isMobile: boolean): ViewMode {
  // Mobile defaults to list (map is hard to use on small screens)
  if (isMobile) return 'list'
  try {
    const v = localStorage.getItem(LS_VIEW_MODE_KEY)
    if (v === 'map' || v === 'list') return v
  } catch { /* noop */ }
  return 'map'
}

function writeViewMode(v: ViewMode) {
  try { localStorage.setItem(LS_VIEW_MODE_KEY, v) } catch { /* noop */ }
}

function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 1024)
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 1023px)')
    const handler = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])
  return isMobile
}

// ── Tab bar ───────────────────────────────────────────────────────────────────

interface TabBarProps {
  activeTab: TopologyTab
  onTabChange: (tab: TopologyTab) => void
  scheduledCount: number
  orphanPublishersCount: number
  orphanSubscribersCount: number
  orphanPublishersLoading: boolean
  orphanSubscribersLoading: boolean
}

function TabBar({
  activeTab,
  onTabChange,
  scheduledCount,
  orphanPublishersCount,
  orphanSubscribersCount,
  orphanPublishersLoading,
  orphanSubscribersLoading,
}: TabBarProps) {
  const tabs: {
    id: TopologyTab
    label: string
    count: number | null
    loading?: boolean
    ariaLabel: string
  }[] = [
    { id: 'all', label: 'All', count: null, ariaLabel: 'All topics and queues' },
    {
      id: 'orphan-publishers',
      label: 'Orphan Publishers',
      count: orphanPublishersLoading ? null : orphanPublishersCount,
      loading: orphanPublishersLoading,
      ariaLabel: `Orphan Publishers${orphanPublishersCount > 0 ? `, ${orphanPublishersCount} found` : ''}`,
    },
    {
      id: 'orphan-subscribers',
      label: 'Orphan Subscribers',
      count: orphanSubscribersLoading ? null : orphanSubscribersCount,
      loading: orphanSubscribersLoading,
      ariaLabel: `Orphan Subscribers${orphanSubscribersCount > 0 ? `, ${orphanSubscribersCount} found` : ''}`,
    },
    {
      id: 'scheduled',
      label: 'Scheduled Jobs',
      count: scheduledCount > 0 ? scheduledCount : null,
      ariaLabel: `Scheduled Jobs${scheduledCount > 0 ? `, ${scheduledCount} found` : ''}`,
    },
  ]

  return (
    <div
      role="tablist"
      aria-label="Topology views"
      className="flex items-center gap-0 border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 flex-shrink-0"
    >
      {tabs.map((tab) => {
        const isActive = activeTab === tab.id
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={isActive}
            aria-label={tab.ariaLabel}
            id={`topology-tab-${tab.id}`}
            aria-controls={`topology-panel-${tab.id}`}
            onClick={() => onTabChange(tab.id)}
            className={[
              'flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors',
              'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
              isActive
                ? 'border-sky-500 text-sky-600 dark:text-sky-400'
                : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 hover:border-slate-300 dark:hover:border-slate-600',
            ].join(' ')}
          >
            {tab.label}
            {tab.loading ? (
              <span className="inline-flex items-center justify-center px-1.5 py-0.5 text-[10px] rounded-full bg-slate-100 dark:bg-slate-800 text-slate-400 min-w-[20px]">
                …
              </span>
            ) : tab.count !== null && tab.count > 0 ? (
              <span
                className={[
                  'inline-flex items-center justify-center px-1.5 py-0.5 text-[10px] rounded-full min-w-[20px]',
                  isActive
                    ? 'bg-sky-100 dark:bg-sky-900/50 text-sky-700 dark:text-sky-300'
                    : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400',
                ].join(' ')}
              >
                {tab.count}
              </span>
            ) : null}
          </button>
        )
      })}
    </div>
  )
}

/**
 * Surface 3 — Broker Topology route.
 * URL params: ?tab=all|orphan-publishers|orphan-subscribers|scheduled
 *             &protocol=kafka,rabbitmq&topic=<topicId>
 *
 * Layout (All tab):
 *   ┌─ TopNav ──────────────────────────────────────────────────────┐
 *   │ [Tab bar]                                                     │
 *   │ ProtocolFilterChips + search + [Map | List] toggle            │
 *   ├───────────────────────────────────────────────────────────────┤
 *   │ Map view: TopologyMap (flex-1) │ TopicDetailPanel (320px)     │
 *   │           ChannelTrack                                        │
 *   │ List view: TopologyList        │ TopicDetailPanel (320px)     │
 *   └───────────────────────────────┴───────────────────────────────┘
 *
 * Other tabs render their own content below the tab bar.
 */
export function TopologyRoute() {
  const { group } = useParams<{ group: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const [searchQuery, setSearchQuery] = useState('')
  const isMobile = useIsMobile()

  // ── Active tab — persisted to URL query param ──────────────────────────────
  const rawTab = searchParams.get(TAB_PARAM)
  const activeTab: TopologyTab =
    rawTab === 'orphan-publishers' || rawTab === 'orphan-subscribers' || rawTab === 'scheduled'
      ? rawTab
      : 'all'

  const handleTabChange = useCallback((tab: TopologyTab) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      if (tab === 'all') {
        next.delete(TAB_PARAM)
      } else {
        next.set(TAB_PARAM, tab)
      }
      return next
    }, { replace: false })
  }, [setSearchParams])

  // View mode — persisted, defaulting to list on mobile
  const [viewMode, setViewModeRaw] = useState<ViewMode>(() => readViewMode(isMobile))

  const setViewMode = useCallback((next: ViewMode) => {
    setViewModeRaw(next)
    writeViewMode(next)
  }, [])

  // When screen crosses the mobile breakpoint, force list view; restore on desktop
  useEffect(() => {
    if (isMobile) {
      setViewModeRaw('list')
    }
  }, [isMobile])

  const {
    activeProtocols,
    isAllActive,
    toggle,
    setAll,
    selectedTopic: selectedId,
    setSelectedTopic,
  } = useProtocolFilters()

  const {
    data,
    isLoading,
    error,
    refetch,
  } = useTopologyData(group ?? '', {
    protocols: isAllActive ? [] : [...activeProtocols],
  })

  // Orphan publishers / subscribers (independent data fetching per tab)
  const {
    data: orphanPubData,
    isLoading: orphanPubLoading,
  } = useOrphanPublishers(group ?? '')

  const {
    data: orphanSubData,
    isLoading: orphanSubLoading,
  } = useOrphanSubscribers(group ?? '')

  // Derive the chip list from actual data — only show protocols that exist.
  const availableProtocols = useMemo(() => deriveAvailableProtocols(data), [data])

  // Derive scheduled jobs from main topology data
  const scheduledJobs = useMemo(() => deriveScheduledJobs(data), [data])

  const layout = useTopologyLayout(data, activeProtocols)
  const searchResults = useTopologySearch(searchQuery, data)
  const topicDetail = useTopicDetail(selectedId, data)

  // Determine if a GraphQL subscription is selected
  const selectedGqlSub = selectedId
    ? (data?.graphql_subscriptions ?? []).find((g) => g.id === selectedId)
    : undefined

  const gqlPublishers = selectedGqlSub
    ? selectedGqlSub.publisher_ids.flatMap((id) => {
        const s = data?.producers[id] ?? data?.consumers[id]
        return s ? [s] : []
      })
    : []
  const gqlSubscribers = selectedGqlSub
    ? selectedGqlSub.subscriber_ids.flatMap((id) => {
        const s = data?.consumers[id] ?? data?.producers[id]
        return s ? [s] : []
      })
    : []

  // Is the selected item a channel/GQL (shown in channel track, not topology map)?
  const isChannelSelected =
    selectedId &&
    ((data?.channels ?? []).some((c) => c.id === selectedId) ||
      (data?.graphql_subscriptions ?? []).some((g) => g.id === selectedId))

  // In list view, channels/GQL subs are first-class rows — don't exclude them
  const isChannelSelectedInMap = viewMode === 'map' && isChannelSelected

  const orphanPublishers = orphanPubData?.publishers ?? []
  const orphanSubscribers = orphanSubData?.subscribers ?? []

  if (!group) {
    return (
      <div className="h-full flex flex-col items-center justify-center">
        <TopologyEmptyState hasGroup={false} />
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full overflow-hidden bg-white dark:bg-slate-950">
      {/* Tab bar */}
      <TabBar
        activeTab={activeTab}
        onTabChange={handleTabChange}
        scheduledCount={scheduledJobs.length}
        orphanPublishersCount={orphanPublishers.length}
        orphanSubscribersCount={orphanSubscribers.length}
        orphanPublishersLoading={orphanPubLoading}
        orphanSubscribersLoading={orphanSubLoading}
      />

      {/* ── All topics tab ──────────────────────────────────────────────────── */}
      {activeTab === 'all' && (
        <>
          {/* Protocol filter chips + search + view mode toggle */}
          <div
            role="tabpanel"
            id="topology-panel-all"
            aria-labelledby="topology-tab-all"
            className="flex items-center gap-0 border-b border-slate-200 dark:border-slate-800 bg-white/90 dark:bg-slate-950/90 backdrop-blur-sm z-10 flex-shrink-0"
          >
            <ProtocolFilterChips
              allProtocols={availableProtocols}
              activeProtocols={activeProtocols}
              isAllActive={isAllActive}
              onToggle={toggle}
              onSetAll={setAll}
            />

            {/* Typeahead search */}
            <div className="relative px-3 py-1.5 border-l border-slate-200 dark:border-slate-800 flex-shrink-0">
              <div className="flex items-center gap-2 h-7 px-2 rounded bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 focus-within:border-sky-700">
                <Search className="w-3 h-3 text-slate-400 dark:text-slate-500 flex-shrink-0" aria-hidden />
                <input
                  type="search"
                  placeholder="Find topic…"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="bg-transparent text-xs text-slate-800 dark:text-slate-200 placeholder-slate-600 outline-none w-36"
                  aria-label="Search topics and queues"
                  aria-controls="topology-search-results"
                  aria-expanded={searchResults.length > 0}
                />
                {searchQuery && (
                  <button
                    type="button"
                    aria-label="Clear search"
                    onClick={() => setSearchQuery('')}
                    className="text-slate-500 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300"
                  >
                    <X className="w-3 h-3" />
                  </button>
                )}
              </div>

              {/* Search results dropdown — only in map view; list view filters inline */}
              {viewMode === 'map' && searchQuery && searchResults.length > 0 && (
                <ul
                  id="topology-search-results"
                  role="listbox"
                  aria-label="Search results"
                  className="absolute right-3 top-full mt-1 w-72 rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 shadow-xl z-50 max-h-60 overflow-y-auto"
                >
                  {searchResults.map((r) => (
                    <li key={r.id} role="option" aria-selected={r.id === selectedId}>
                      <button
                        type="button"
                        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-slate-200 dark:hover:bg-slate-800 focus:outline-none focus:bg-slate-200 dark:focus:bg-slate-800 transition-colors"
                        onClick={() => {
                          setSelectedTopic(r.id)
                          setSearchQuery('')
                        }}
                      >
                        <span className="font-mono text-xs text-slate-800 dark:text-slate-200 flex-1 truncate">{r.label}</span>
                        <span className="text-xs text-slate-400 dark:text-slate-500 capitalize">{r.protocol}</span>
                        <span className="text-xs text-slate-500 dark:text-slate-600 font-mono">{r.repo}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* View mode toggle — hidden on mobile (locked to list) */}
            {!isMobile && (
              <div className="flex items-center gap-0.5 px-3 py-1.5 border-l border-slate-200 dark:border-slate-800 flex-shrink-0 ml-auto">
                <button
                  type="button"
                  aria-pressed={viewMode === 'map'}
                  title="Map view"
                  onClick={() => setViewMode('map')}
                  className={[
                    'flex items-center gap-1.5 px-2.5 py-1 rounded-l text-xs font-medium border transition-colors',
                    'focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-1 focus:ring-offset-slate-950',
                    viewMode === 'map'
                      ? 'bg-sky-900/50 text-sky-300 border-sky-700 z-10'
                      : 'bg-slate-100 dark:bg-slate-900 text-slate-400 dark:text-slate-500 border-slate-300 dark:border-slate-700 hover:text-slate-700 dark:hover:text-slate-300 hover:border-slate-400 dark:hover:border-slate-500',
                  ].join(' ')}
                >
                  <Map className="w-3.5 h-3.5" aria-hidden />
                  Map
                </button>
                <button
                  type="button"
                  aria-pressed={viewMode === 'list'}
                  title="List view"
                  onClick={() => setViewMode('list')}
                  className={[
                    'flex items-center gap-1.5 px-2.5 py-1 rounded-r text-xs font-medium border -ml-px transition-colors',
                    'focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-1 focus:ring-offset-slate-950',
                    viewMode === 'list'
                      ? 'bg-sky-900/50 text-sky-300 border-sky-700 z-10'
                      : 'bg-slate-100 dark:bg-slate-900 text-slate-400 dark:text-slate-500 border-slate-300 dark:border-slate-700 hover:text-slate-700 dark:hover:text-slate-300 hover:border-slate-400 dark:hover:border-slate-500',
                  ].join(' ')}
                >
                  <List className="w-3.5 h-3.5" aria-hidden />
                  List
                </button>
              </div>
            )}
          </div>

          {/* Main content area */}
          <div className="flex flex-1 overflow-hidden">
            {/* Canvas / list column */}
            <div className="flex flex-col flex-1 overflow-hidden">
              {isLoading ? (
                <TopologyLoadingState />
              ) : error ? (
                <div className="flex-1 flex items-center justify-center">
                  <TopologyErrorState error={error} onRetry={() => void refetch()} />
                </div>
              ) : !data || (
                (data.topics ?? []).length === 0 &&
                (data.queues ?? []).length === 0 &&
                (data.nats_subjects ?? []).length === 0 &&
                (data.channels ?? []).length === 0 &&
                (data.graphql_subscriptions ?? []).length === 0 &&
                (data.functions ?? []).length === 0
              ) ? (
                <div className="flex-1 flex items-center justify-center">
                  <TopologyEmptyState hasFilters={!isAllActive} onClearFilters={setAll} />
                </div>
              ) : viewMode === 'list' ? (
                /* ── List view ─────────────────────────────────────────────── */
                <TopologyList
                  data={data!}
                  searchQuery={searchQuery}
                  selectedId={selectedId}
                  onSelectEntity={setSelectedTopic}
                />
              ) : (
                /* ── Map view ─────────────────────────────────────────────── */
                <>
                  {/* Force-layout canvas — only broker topics, not channels */}
                  <div className="flex-1 overflow-hidden relative">
                    <TopologyMap
                      layout={layout}
                      data={data!}
                      selectedId={isChannelSelectedInMap ? null : selectedId}
                      onSelectTopic={setSelectedTopic}
                    />
                  </div>

                  {/* Channel track (WebSocket/SSE/GraphQL) */}
                  {((data.channels ?? []).length > 0 || (data.graphql_subscriptions ?? []).length > 0) && (
                    <ChannelTrack
                      channels={data.channels ?? []}
                      graphqlSubscriptions={data.graphql_subscriptions ?? []}
                      selectedId={isChannelSelected ? selectedId : null}
                      onSelect={setSelectedTopic}
                    />
                  )}
                </>
              )}
            </div>

            {/* Right panel: topic detail or GraphQL subscription panel */}
            {selectedId && !isChannelSelectedInMap && topicDetail.node && (
              <TopicDetailPanel
                detail={topicDetail}
                onClose={() => setSelectedTopic(null)}
                onNavigateToTopic={(id) => {
                  setSelectedTopic(id)
                }}
              />
            )}

            {/* In map view: GraphQL subscription panel for channel-track selections */}
            {viewMode === 'map' && selectedGqlSub && (
              <GraphQLSubscriptionPanel
                subscription={selectedGqlSub}
                publishers={gqlPublishers}
                subscribers={gqlSubscribers}
                onClose={() => setSelectedTopic(null)}
              />
            )}
          </div>
        </>
      )}

      {/* ── Orphan Publishers tab ───────────────────────────────────────────── */}
      {activeTab === 'orphan-publishers' && (
        <div
          role="tabpanel"
          id="topology-panel-orphan-publishers"
          aria-labelledby="topology-tab-orphan-publishers"
          className="flex flex-1 overflow-hidden"
        >
          <OrphanPublishersTab
            publishers={orphanPublishers}
            isLoading={orphanPubLoading}
          />
        </div>
      )}

      {/* ── Orphan Subscribers tab ──────────────────────────────────────────── */}
      {activeTab === 'orphan-subscribers' && (
        <div
          role="tabpanel"
          id="topology-panel-orphan-subscribers"
          aria-labelledby="topology-tab-orphan-subscribers"
          className="flex flex-1 overflow-hidden"
        >
          <OrphanSubscribersTab
            subscribers={orphanSubscribers}
            isLoading={orphanSubLoading}
          />
        </div>
      )}

      {/* ── Scheduled Jobs tab ──────────────────────────────────────────────── */}
      {activeTab === 'scheduled' && (
        <div
          role="tabpanel"
          id="topology-panel-scheduled"
          aria-labelledby="topology-tab-scheduled"
          className="flex flex-1 overflow-hidden"
        >
          {isLoading ? (
            <TopologyLoadingState />
          ) : (
            <ScheduledJobsTab jobs={scheduledJobs} />
          )}
        </div>
      )}
    </div>
  )
}
