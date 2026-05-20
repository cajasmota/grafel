import { PROTOCOL_COLORS } from '@/lib/colors'
import type { TopologyProtocol } from '@/types/api'

interface ProtocolFilterChipsProps {
  allProtocols: TopologyProtocol[]
  activeProtocols: Set<TopologyProtocol>
  isAllActive: boolean
  onToggle: (protocol: TopologyProtocol) => void
  onSetAll: () => void
}

/**
 * Multi-select chip row for filtering by broker/channel protocol.
 * Keyboard-accessible: Tab between chips, Space/Enter to toggle.
 * Color is not the sole differentiator — each chip shows a protocol-specific shape glyph + label.
 */
export function ProtocolFilterChips({
  allProtocols,
  activeProtocols,
  isAllActive,
  onToggle,
  onSetAll,
}: ProtocolFilterChipsProps) {
  return (
    <div
      role="group"
      aria-label="Filter by protocol"
      className="flex items-center gap-1.5 flex-wrap px-4 py-2 border-b border-slate-800"
    >
      {/* "All" chip */}
      <button
        type="button"
        role="checkbox"
        aria-checked={isAllActive}
        onClick={onSetAll}
        className={[
          'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium border transition-colors',
          'focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-1 focus:ring-offset-slate-950',
          isAllActive
            ? 'bg-slate-700 text-slate-200 border-slate-500'
            : 'bg-slate-900 text-slate-500 border-slate-700 hover:border-slate-500 hover:text-slate-300',
        ].join(' ')}
      >
        All
      </button>

      {allProtocols.map((protocol) => {
        const spec = PROTOCOL_COLORS[protocol]
        const isActive = activeProtocols.has(protocol)

        return (
          <button
            key={protocol}
            type="button"
            role="checkbox"
            aria-checked={isActive}
            onClick={() => onToggle(protocol)}
            className={[
              'inline-flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium border transition-colors',
              'focus:outline-none focus:ring-2 focus:ring-offset-1 focus:ring-offset-slate-950',
              isActive
                ? `${spec.bg} ${spec.text} ${spec.border}`
                : 'bg-slate-900 text-slate-500 border-slate-700 hover:border-slate-500 hover:text-slate-300',
            ].join(' ')}
            style={isActive ? {} : undefined}
          >
            <ProtocolShapeGlyph protocol={protocol} hex={spec.hex} isActive={isActive} />
            {spec.label}
          </button>
        )
      })}
    </div>
  )
}

/**
 * Small SVG shape glyph — protocol shape is the non-color differentiator.
 * Square=Kafka, Circle=RabbitMQ, Hexagon=SQS, Diamond=Pub/Sub,
 * Pentagon=NATS, Triangle=WebSocket, Star=SSE, Cross=GraphQL Sub.
 */
function ProtocolShapeGlyph({
  protocol,
  hex,
  isActive,
}: {
  protocol: TopologyProtocol
  hex: string
  isActive: boolean
}) {
  const fill = isActive ? hex : '#64748b'
  const size = 8

  switch (protocol) {
    case 'kafka':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <rect x="1" y="1" width="6" height="6" fill={fill} />
        </svg>
      )
    case 'rabbitmq':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <circle cx="4" cy="4" r="3.5" fill={fill} />
        </svg>
      )
    case 'sqs':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <polygon points="4,0.5 7.5,2.5 7.5,5.5 4,7.5 0.5,5.5 0.5,2.5" fill={fill} />
        </svg>
      )
    case 'pubsub':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <polygon points="4,0.5 7.5,4 4,7.5 0.5,4" fill={fill} />
        </svg>
      )
    case 'nats':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <polygon points="4,0.5 7.5,3 6.4,7 1.6,7 0.5,3" fill={fill} />
        </svg>
      )
    case 'websocket':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <polygon points="4,0.5 7.5,7.5 0.5,7.5" fill={fill} />
        </svg>
      )
    case 'sse':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <polygon points="4,0.5 5.8,2.8 8,3 6.2,4.8 6.5,7 4,5.8 1.5,7 1.8,4.8 0,3 2.2,2.8" fill={fill} />
        </svg>
      )
    case 'graphql_subscription':
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <path d="M3,0.5 H5 V3 H7.5 V5 H5 V7.5 H3 V5 H0.5 V3 H3 Z" fill={fill} />
        </svg>
      )
    default:
      return (
        <svg width={size} height={size} viewBox="0 0 8 8" aria-hidden>
          <circle cx="4" cy="4" r="3.5" fill={fill} />
        </svg>
      )
  }
}
