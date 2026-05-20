import { X, ArrowUp, ArrowDown, ArrowRight, ExternalLink } from 'lucide-react'
import { RepoChip } from '@/components/shared/RepoChip'
import { PROTOCOL_COLORS } from '@/lib/colors'
import type { TopicDetailData } from '@/hooks/topology/useTopicDetail'
import type { TopologyEntityStub, TopicNode, QueueNode, NatsSubject } from '@/types/api'

interface TopicDetailPanelProps {
  detail: TopicDetailData
  onClose: () => void
  onNavigateToTopic: (id: string) => void
}

/**
 * Slide-in panel showing full topic/queue/subject detail:
 * producers list, consumers list, transform chains, protocol metadata.
 * Keyboard: Esc closes (handled by parent), Tab navigates within.
 */
export function TopicDetailPanel({ detail, onClose, onNavigateToTopic }: TopicDetailPanelProps) {
  const { node, producers, consumers, transformsTo } = detail
  if (!node) return null

  const protocol = 'broker' in node
    ? (node as TopicNode | QueueNode | NatsSubject).broker
    : 'channel_type' in node
      ? node.channel_type
      : 'graphql_subscription'

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const spec = PROTOCOL_COLORS[protocol as keyof typeof PROTOCOL_COLORS]
  const label = node.label

  // Determine metadata fields
  const queueType = 'queue_type' in node ? (node as QueueNode).queue_type : undefined
  const serverEndpoint = 'server_endpoint' in node ? (node as import('@/types/api').ChannelNode).server_endpoint : undefined
  const returnType = 'return_type' in node ? (node as import('@/types/api').GraphQLSubscription).return_type : undefined

  return (
    <aside
      className="w-80 flex flex-col border-l border-slate-800 bg-slate-950 overflow-y-auto"
      aria-label={`Topic detail: ${label}`}
    >
      {/* Header */}
      <div className={`flex items-start gap-2 px-4 py-3 border-b border-slate-800 ${spec.bg}`}>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium ${spec.bg} ${spec.text} ${spec.border} border`}>
              {spec.label}
            </span>
            {queueType && (
              <span className="text-xs text-slate-500 capitalize">{queueType}</span>
            )}
          </div>
          <p className="font-mono text-sm text-slate-100 truncate" title={label}>
            {label}
          </p>
          {serverEndpoint && (
            <p className="font-mono text-xs text-slate-400 truncate mt-0.5" title={serverEndpoint}>
              {serverEndpoint}
            </p>
          )}
          {returnType && (
            <p className="font-mono text-xs text-slate-400 mt-0.5">→ {returnType}</p>
          )}
        </div>
        <button
          type="button"
          aria-label="Close topic detail panel"
          onClick={onClose}
          className="p-1.5 rounded text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors flex-shrink-0 mt-0.5"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Metadata row */}
      <div className="flex items-center gap-3 px-4 py-2 border-b border-slate-800 text-xs">
        <RepoChip repo={node.repo} />
        <span className="text-slate-600">
          {producers.length} producer{producers.length !== 1 ? 's' : ''}
        </span>
        <span className="text-slate-600">
          {consumers.length} consumer{consumers.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Producers */}
      <EntitySection
        title="Producers"
        icon={<ArrowUp className="w-3.5 h-3.5" />}
        entities={producers}
        emptyMessage="No producers found"
        arrowLabel="produces into this topic"
      />

      {/* Consumers */}
      <EntitySection
        title="Consumers"
        icon={<ArrowDown className="w-3.5 h-3.5" />}
        entities={consumers}
        emptyMessage="No consumers found"
        arrowLabel="consumes from this topic"
      />

      {/* Transform chains */}
      {transformsTo.length > 0 && (
        <div className="flex flex-col border-b border-slate-800">
          <div className="flex items-center gap-1.5 px-4 py-2 bg-amber-950/20">
            <ArrowRight className="w-3.5 h-3.5 text-amber-400" aria-hidden />
            <span className="text-xs font-medium text-amber-300">Transforms To</span>
            <span className="ml-auto text-xs text-amber-600">{transformsTo.length}</span>
          </div>
          <ul className="divide-y divide-slate-800/60">
            {transformsTo.map((t) => (
              <li key={t.id} className="flex items-center gap-2.5 px-4 py-2.5">
                <div className="flex-1 min-w-0">
                  <p className="font-mono text-xs text-amber-200 truncate" title={t.label}>
                    {t.label}
                  </p>
                  <p className="text-xs text-slate-500 capitalize">
                    {'broker' in t ? (t as TopicNode | QueueNode).broker : 'nats'}
                  </p>
                </div>
                <button
                  type="button"
                  aria-label={`Navigate to ${t.label}`}
                  onClick={() => onNavigateToTopic(t.id)}
                  className="p-1 rounded text-slate-500 hover:text-amber-300 hover:bg-slate-800 transition-colors"
                >
                  <ExternalLink className="w-3 h-3" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </aside>
  )
}

function EntitySection({
  title,
  icon,
  entities,
  emptyMessage,
  arrowLabel,
}: {
  title: string
  icon: React.ReactNode
  entities: TopologyEntityStub[]
  emptyMessage: string
  arrowLabel: string
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
        <ul className="divide-y divide-slate-800/60" aria-label={arrowLabel}>
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
