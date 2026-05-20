import { useState, useCallback, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { useTopologyData } from '@/hooks/topology/useTopologyData'
import { useTopicDetail } from '@/hooks/topology/useTopicDetail'
import { useProtocolFilters } from '@/hooks/topology/useProtocolFilters'
import { useTopologyLayout } from '@/hooks/topology/useTopologyLayout'
import { useTopologySearch } from '@/hooks/topology/useTopologySearch'
import { ProtocolFilterChips } from '@/components/topology/ProtocolFilterChips'
import { TopologyMap } from '@/components/topology/TopologyMap'
import { TopologyList } from '@/components/topology/TopologyList'
import { TopicDetailPanel } from '@/components/topology/TopicDetailPanel'
import { ChannelTrack } from '@/components/topology/ChannelTrack'
import { GraphQLSubscriptionPanel } from '@/components/topology/GraphQLSubscriptionPanel'
import { TopologyLoadingState } from '@/components/topology/TopologyLoadingState'
import { TopologyEmptyState } from '@/components/topology/TopologyEmptyState'
import { TopologyErrorState } from '@/components/topology/TopologyErrorState'
import { Search, X, Map, List } from 'lucide-react'

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

/**
 * Surface 3 — Broker Topology route.
 * URL params: ?protocol=kafka,rabbitmq&topic=<topicId>
 *
 * Layout:
 *   ┌─ TopNav ──────────────────────────────────────────────────────┐
 *   │ ProtocolFilterChips + search + [Map | List] toggle            │
 *   ├───────────────────────────────────────────────────────────────┤
 *   │ Map view: TopologyMap (flex-1) │ TopicDetailPanel (320px)     │
 *   │           ChannelTrack                                        │
 *   │ List view: TopologyList        │ TopicDetailPanel (320px)     │
 *   └───────────────────────────────┴───────────────────────────────┘
 */
export function TopologyRoute() {
  const { group } = useParams<{ group: string }>()
  const [searchQuery, setSearchQuery] = useState('')
  const isMobile = useIsMobile()

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
    allProtocols,
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

  if (!group) {
    return (
      <div className="h-full flex flex-col items-center justify-center">
        <TopologyEmptyState hasGroup={false} />
      </div>
    )
  }

  const isEmpty = !data || (
    data.topics.length === 0 &&
    data.queues.length === 0 &&
    data.nats_subjects.length === 0 &&
    data.channels.length === 0 &&
    data.graphql_subscriptions.length === 0
  )

  return (
    <div className="flex flex-col h-full overflow-hidden bg-slate-950">
      {/* Protocol filter chips + search + view mode toggle */}
      <div className="flex items-center gap-0 border-b border-slate-800 bg-slate-950/90 backdrop-blur-sm z-10 flex-shrink-0">
        <ProtocolFilterChips
          allProtocols={allProtocols}
          activeProtocols={activeProtocols}
          isAllActive={isAllActive}
          onToggle={toggle}
          onSetAll={setAll}
        />

        {/* Typeahead search */}
        <div className="relative px-3 py-1.5 border-l border-slate-800 flex-shrink-0">
          <div className="flex items-center gap-2 h-7 px-2 rounded bg-slate-900 border border-slate-700 focus-within:border-sky-700">
            <Search className="w-3 h-3 text-slate-500 flex-shrink-0" aria-hidden />
            <input
              type="search"
              placeholder="Find topic…"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="bg-transparent text-xs text-slate-200 placeholder-slate-600 outline-none w-36"
              aria-label="Search topics and queues"
              aria-controls="topology-search-results"
              aria-expanded={searchResults.length > 0}
            />
            {searchQuery && (
              <button
                type="button"
                aria-label="Clear search"
                onClick={() => setSearchQuery('')}
                className="text-slate-600 hover:text-slate-300"
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
              className="absolute right-3 top-full mt-1 w-72 rounded-lg bg-slate-900 border border-slate-700 shadow-xl z-50 max-h-60 overflow-y-auto"
            >
              {searchResults.map((r) => (
                <li key={r.id} role="option" aria-selected={r.id === selectedId}>
                  <button
                    type="button"
                    className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-slate-800 focus:outline-none focus:bg-slate-800 transition-colors"
                    onClick={() => {
                      setSelectedTopic(r.id)
                      setSearchQuery('')
                    }}
                  >
                    <span className="font-mono text-xs text-slate-200 flex-1 truncate">{r.label}</span>
                    <span className="text-xs text-slate-500 capitalize">{r.protocol}</span>
                    <span className="text-xs text-slate-600 font-mono">{r.repo}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* View mode toggle — hidden on mobile (locked to list) */}
        {!isMobile && (
          <div className="flex items-center gap-0.5 px-3 py-1.5 border-l border-slate-800 flex-shrink-0 ml-auto">
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
                  : 'bg-slate-900 text-slate-500 border-slate-700 hover:text-slate-300 hover:border-slate-500',
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
                  : 'bg-slate-900 text-slate-500 border-slate-700 hover:text-slate-300 hover:border-slate-500',
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
    </div>
  )
}
