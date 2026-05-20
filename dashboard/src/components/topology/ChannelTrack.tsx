import { Wifi, Radio, Zap } from 'lucide-react'
import { RepoChip } from '@/components/shared/RepoChip'
import { PROTOCOL_COLORS } from '@/lib/colors'
import type { ChannelNode, GraphQLSubscription } from '@/types/api'

/** Support both *_ids (TypeScript type) and plain * (REST API) field names */
function getIds(node: unknown, idsKey: string, plainKey: string): string[] {
  const r = node as Record<string, unknown>
  return (r[idsKey] as string[] | undefined) ?? (r[plainKey] as string[] | undefined) ?? []
}

interface ChannelTrackProps {
  channels: ChannelNode[]
  graphqlSubscriptions: GraphQLSubscription[]
  selectedId: string | null
  onSelect: (id: string) => void
}

/**
 * Separate visual track for WebSocket/SSE channels and GraphQL subscriptions.
 * These are a different protocol family from broker topics and render as a
 * horizontal card list rather than force-graph nodes.
 * Keyboard-accessible: Tab to navigate, Enter/Space to select.
 */
export function ChannelTrack({ channels, graphqlSubscriptions, selectedId, onSelect }: ChannelTrackProps) {
  const hasChannels = channels.length > 0 || graphqlSubscriptions.length > 0
  if (!hasChannels) return null

  return (
    <div className="border-t border-slate-800 bg-slate-950/60">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800/60">
        <Wifi className="w-3.5 h-3.5 text-slate-500" aria-hidden />
        <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">
          Real-time Channels
        </span>
        <span className="ml-auto text-xs text-slate-600">
          {channels.length + graphqlSubscriptions.length} channel{channels.length + graphqlSubscriptions.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div
        role="list"
        className="flex gap-2 px-4 py-2 overflow-x-auto"
        aria-label="Real-time channels"
      >
        {channels.map((ch) => (
          <ChannelCard
            key={ch.id}
            id={ch.id}
            label={ch.label}
            channelType={ch.channel_type}
            repo={ch.repo}
            endpoint={ch.server_endpoint}
            emitterCount={getIds(ch, 'emitter_ids', 'emitters').length}
            subscriberCount={getIds(ch, 'subscriber_ids', 'subscribers').length}
            isSelected={ch.id === selectedId}
            onSelect={onSelect}
          />
        ))}
        {graphqlSubscriptions.map((sub) => (
          <GraphQLSubCard
            key={sub.id}
            id={sub.id}
            label={sub.label}
            repo={sub.repo}
            returnType={sub.return_type}
            publisherCount={getIds(sub, 'publisher_ids', 'publishers').length}
            subscriberCount={getIds(sub, 'subscriber_ids', 'subscribers').length}
            isSelected={sub.id === selectedId}
            onSelect={onSelect}
          />
        ))}
      </div>
    </div>
  )
}

interface ChannelCardProps {
  id: string
  label: string
  channelType: 'websocket' | 'sse' | 'graphql_subscription' | 'redis_pubsub'
  repo: string
  endpoint?: string
  emitterCount: number
  subscriberCount: number
  isSelected: boolean
  onSelect: (id: string) => void
}

function ChannelCard({
  id,
  label,
  channelType,
  repo,
  endpoint,
  emitterCount,
  subscriberCount,
  isSelected,
  onSelect,
}: ChannelCardProps) {
  const spec = PROTOCOL_COLORS[channelType]
  const ChannelIcon = channelType === 'websocket' ? Wifi : channelType === 'sse' ? Radio : Zap

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect(id)
    }
  }

  return (
    <div
      role="listitem"
      tabIndex={0}
      aria-selected={isSelected}
      aria-label={`${spec.label} channel: ${label}`}
      onClick={() => onSelect(id)}
      onKeyDown={handleKeyDown}
      className={[
        'flex-shrink-0 w-52 rounded-lg border p-3 cursor-pointer transition-colors',
        'focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-1 focus:ring-offset-slate-950',
        isSelected
          ? `${spec.bg} ${spec.border} border`
          : 'bg-slate-900 border-slate-800 hover:border-slate-600',
      ].join(' ')}
    >
      {/* Header row */}
      <div className="flex items-center gap-2 mb-2">
        <div className={`p-1 rounded ${spec.bg}`}>
          <ChannelIcon className={`w-3 h-3 ${spec.text}`} aria-hidden />
        </div>
        <span className={`text-xs font-medium px-1.5 py-0.5 rounded ${spec.bg} ${spec.text}`}>
          {spec.label}
        </span>
      </div>

      {/* Label */}
      <p className="font-mono text-xs text-slate-200 truncate mb-1" title={label}>
        {label}
      </p>

      {/* Endpoint if present */}
      {endpoint && (
        <p className="font-mono text-xs text-slate-500 truncate mb-2" title={endpoint}>
          {endpoint}
        </p>
      )}

      {/* Stats row */}
      <div className="flex items-center gap-3 text-xs text-slate-500">
        <span title={`${emitterCount} emitter${emitterCount !== 1 ? 's' : ''}`}>
          ↑ {emitterCount}
        </span>
        <span title={`${subscriberCount} subscriber${subscriberCount !== 1 ? 's' : ''}`}>
          ↓ {subscriberCount}
        </span>
        <RepoChip repo={repo} className="ml-auto" />
      </div>
    </div>
  )
}

interface GraphQLSubCardProps {
  id: string
  label: string
  repo: string
  returnType?: string
  publisherCount: number
  subscriberCount: number
  isSelected: boolean
  onSelect: (id: string) => void
}

function GraphQLSubCard({
  id,
  label,
  repo,
  returnType,
  publisherCount,
  subscriberCount,
  isSelected,
  onSelect,
}: GraphQLSubCardProps) {
  const spec = PROTOCOL_COLORS['graphql_subscription']

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect(id)
    }
  }

  return (
    <div
      role="listitem"
      tabIndex={0}
      aria-selected={isSelected}
      aria-label={`GraphQL subscription: ${label}`}
      onClick={() => onSelect(id)}
      onKeyDown={handleKeyDown}
      className={[
        'flex-shrink-0 w-52 rounded-lg border p-3 cursor-pointer transition-colors',
        'focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-1 focus:ring-offset-slate-950',
        isSelected
          ? `${spec.bg} ${spec.border} border`
          : 'bg-slate-900 border-slate-800 hover:border-slate-600',
      ].join(' ')}
    >
      {/* Header */}
      <div className="flex items-center gap-2 mb-2">
        <div className={`p-1 rounded ${spec.bg}`}>
          <Zap className={`w-3 h-3 ${spec.text}`} aria-hidden />
        </div>
        <span className={`text-xs font-medium px-1.5 py-0.5 rounded ${spec.bg} ${spec.text}`}>
          {spec.label}
        </span>
      </div>

      <p className="font-mono text-xs text-slate-200 truncate mb-1" title={label}>
        subscription {label}
      </p>

      {returnType && (
        <p className="font-mono text-xs text-slate-500 truncate mb-2" title={returnType}>
          → {returnType}
        </p>
      )}

      <div className="flex items-center gap-3 text-xs text-slate-500">
        <span title={`${publisherCount} publisher${publisherCount !== 1 ? 's' : ''}`}>
          ↑ {publisherCount}
        </span>
        <span title={`${subscriberCount} subscriber${subscriberCount !== 1 ? 's' : ''}`}>
          ↓ {subscriberCount}
        </span>
        <RepoChip repo={repo} className="ml-auto" />
      </div>
    </div>
  )
}
