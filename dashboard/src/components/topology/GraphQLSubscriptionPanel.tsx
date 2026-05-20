import { X, Zap, ArrowUp, ArrowDown } from 'lucide-react'
import { RepoChip } from '@/components/shared/RepoChip'
import { PROTOCOL_COLORS } from '@/lib/colors'
import type { GraphQLSubscription, TopologyEntityStub } from '@/types/api'

interface GraphQLSubscriptionPanelProps {
  subscription: GraphQLSubscription
  publishers: TopologyEntityStub[]
  subscribers: TopologyEntityStub[]
  onClose: () => void
}

/**
 * Detail panel for a GraphQL subscription.
 * Distinct from ChannelTrack cards — this is the drill-down.
 * Shown when a GraphQL subscription node is selected from ChannelTrack.
 */
export function GraphQLSubscriptionPanel({
  subscription,
  publishers,
  subscribers,
  onClose,
}: GraphQLSubscriptionPanelProps) {
  const spec = PROTOCOL_COLORS['graphql_subscription']

  return (
    <aside
      className="w-80 flex flex-col border-l border-slate-800 bg-slate-950 overflow-y-auto"
      aria-label={`GraphQL subscription detail: ${subscription.label}`}
    >
      {/* Header */}
      <div className={`flex items-center gap-2 px-4 py-3 border-b border-slate-800 ${spec.bg}`}>
        <div className={`p-1.5 rounded ${spec.bg}`}>
          <Zap className={`w-4 h-4 ${spec.text}`} aria-hidden />
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-xs text-slate-400">{spec.label}</p>
          <p className="font-mono text-sm text-slate-100 truncate" title={subscription.label}>
            subscription {subscription.label}
          </p>
        </div>
        <button
          type="button"
          aria-label="Close GraphQL subscription panel"
          onClick={onClose}
          className="p-1.5 rounded text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors flex-shrink-0"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Schema info */}
      <div className="px-4 py-3 border-b border-slate-800 space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="text-slate-500">Schema type</span>
          <span className="font-mono text-slate-300">{subscription.schema_type}</span>
        </div>
        {subscription.return_type && (
          <div className="flex items-center justify-between text-xs">
            <span className="text-slate-500">Return type</span>
            <span className="font-mono text-slate-300">{subscription.return_type}</span>
          </div>
        )}
        <div className="flex items-center justify-between text-xs">
          <span className="text-slate-500">Repo</span>
          <RepoChip repo={subscription.repo} />
        </div>
      </div>

      {/* Publishers */}
      <EntityList
        title="Publishers"
        icon={<ArrowUp className="w-3.5 h-3.5" />}
        entities={publishers}
        emptyMessage="No publishers found"
      />

      {/* Subscribers */}
      <EntityList
        title="Subscribers"
        icon={<ArrowDown className="w-3.5 h-3.5" />}
        entities={subscribers}
        emptyMessage="No subscribers found"
      />
    </aside>
  )
}

function EntityList({
  title,
  icon,
  entities,
  emptyMessage,
}: {
  title: string
  icon: React.ReactNode
  entities: TopologyEntityStub[]
  emptyMessage: string
}) {
  return (
    <div className="flex flex-col border-b border-slate-800 last:border-b-0">
      <div className="flex items-center gap-1.5 px-4 py-2 bg-slate-900/40">
        <span className="text-slate-500">{icon}</span>
        <span className="text-xs font-medium text-slate-400">{title}</span>
        <span className="ml-auto text-xs text-slate-600">{entities.length}</span>
      </div>
      {entities.length === 0 ? (
        <p className="px-4 py-3 text-xs text-slate-600 italic">{emptyMessage}</p>
      ) : (
        <ul className="divide-y divide-slate-800/60">
          {entities.map((e) => (
            <li key={e.id} className="flex items-start gap-2.5 px-4 py-2.5">
              <div className="flex-1 min-w-0">
                <p className="font-mono text-xs text-slate-200 truncate" title={e.label}>
                  {e.label}
                </p>
                <p className="text-xs text-slate-500 truncate" title={e.source_file}>
                  {e.source_file}:{e.start_line}
                </p>
              </div>
              <RepoChip repo={e.repo} className="flex-shrink-0 mt-0.5" />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
