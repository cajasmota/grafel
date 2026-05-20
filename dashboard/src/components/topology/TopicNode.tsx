import { PROTOCOL_COLORS } from '@/lib/colors'
import type { TopologyProtocol } from '@/types/api'

interface TopicNodeProps {
  id: string
  label: string
  protocol: TopologyProtocol
  spokeCount: number
  isSelected: boolean
  isFocused: boolean
  x: number
  y: number
  onClick: (id: string) => void
  onKeyDown: (e: React.KeyboardEvent, id: string) => void
}

/**
 * SVG topic/queue/subject hub node.
 * Size is proportional to spokeCount (producer + consumer count).
 * Shape and color encode protocol — never relies on color alone.
 * Keyboard-accessible: receives focus + Enter/arrow navigation.
 */
export function TopicNode({
  id,
  label,
  protocol,
  spokeCount,
  isSelected,
  isFocused,
  x,
  y,
  onClick,
  onKeyDown,
}: TopicNodeProps) {
  const spec = PROTOCOL_COLORS[protocol]
  // Node radius scales with spoke count, clamped to [16, 36]
  const r = Math.min(36, Math.max(16, 12 + spokeCount * 2.5))
  const strokeWidth = isSelected ? 2.5 : isFocused ? 2 : 1.5
  const strokeColor = isSelected ? '#38bdf8' : isFocused ? '#7dd3fc' : spec.hex

  return (
    <g
      role="button"
      tabIndex={0}
      aria-label={`${spec.label} topic: ${label} — ${spokeCount} connections`}
      aria-pressed={isSelected}
      transform={`translate(${x},${y})`}
      style={{ cursor: 'pointer' }}
      onClick={() => onClick(id)}
      onKeyDown={(e) => onKeyDown(e, id)}
    >
      {/* Focus ring */}
      {(isSelected || isFocused) && (
        <circle r={r + 6} fill="none" stroke={strokeColor} strokeWidth={1} strokeDasharray="3,2" opacity={0.5} />
      )}

      {/* Protocol shape */}
      <NodeShape protocol={protocol} r={r} fill={spec.hex + '33'} stroke={strokeColor} strokeWidth={strokeWidth} />

      {/* Protocol icon abbreviation (non-color differentiator label) */}
      <text
        textAnchor="middle"
        dominantBaseline="central"
        fontSize={Math.max(8, r * 0.38)}
        fill={spec.hex}
        fontFamily="ui-monospace, monospace"
        fontWeight="600"
        aria-hidden
      >
        {protocolAbbrev(protocol)}
      </text>

      {/* Topic label below node */}
      <text
        y={r + 12}
        textAnchor="middle"
        fontSize={10}
        fill="#cbd5e1"
        fontFamily="ui-sans-serif, system-ui, sans-serif"
        aria-hidden
      >
        {truncateLabel(label, 18)}
      </text>
    </g>
  )
}

function NodeShape({
  protocol,
  r,
  fill,
  stroke,
  strokeWidth,
}: {
  protocol: TopologyProtocol
  r: number
  fill: string
  stroke: string
  strokeWidth: number
}) {
  const commonProps = { fill, stroke, strokeWidth }

  switch (protocol) {
    case 'kafka':
      return <rect x={-r} y={-r} width={r * 2} height={r * 2} rx={3} {...commonProps} />
    case 'rabbitmq':
      return <circle r={r} {...commonProps} />
    case 'sqs': {
      const pts = hexagonPoints(r)
      return <polygon points={pts} {...commonProps} />
    }
    case 'pubsub': {
      const pts = diamondPoints(r)
      return <polygon points={pts} {...commonProps} />
    }
    case 'nats': {
      const pts = pentagonPoints(r)
      return <polygon points={pts} {...commonProps} />
    }
    case 'websocket': {
      const pts = trianglePoints(r)
      return <polygon points={pts} {...commonProps} />
    }
    case 'sse': {
      const pts = starPoints(r, 5, r * 0.5)
      return <polygon points={pts} {...commonProps} />
    }
    case 'graphql_subscription': {
      const w = r * 0.55
      return (
        <path
          d={`M${-w},${-r} H${w} V${-w} H${r} V${w} H${w} V${r} H${-w} V${w} H${-r} V${-w} H${-w} Z`}
          {...commonProps}
        />
      )
    }
    default:
      return <circle r={r} {...commonProps} />
  }
}

function hexagonPoints(r: number): string {
  return Array.from({ length: 6 }, (_, i) => {
    const angle = (Math.PI / 3) * i - Math.PI / 6
    return `${r * Math.cos(angle)},${r * Math.sin(angle)}`
  }).join(' ')
}

function diamondPoints(r: number): string {
  return `0,${-r} ${r},0 0,${r} ${-r},0`
}

function pentagonPoints(r: number): string {
  return Array.from({ length: 5 }, (_, i) => {
    const angle = (2 * Math.PI * i) / 5 - Math.PI / 2
    return `${r * Math.cos(angle)},${r * Math.sin(angle)}`
  }).join(' ')
}

function trianglePoints(r: number): string {
  return `0,${-r} ${r * 0.866},${r * 0.5} ${-r * 0.866},${r * 0.5}`
}

function starPoints(outerR: number, points: number, innerR: number): string {
  return Array.from({ length: points * 2 }, (_, i) => {
    const angle = (Math.PI * i) / points - Math.PI / 2
    const radius = i % 2 === 0 ? outerR : innerR
    return `${radius * Math.cos(angle)},${radius * Math.sin(angle)}`
  }).join(' ')
}

function protocolAbbrev(protocol: TopologyProtocol): string {
  const map: Record<TopologyProtocol, string> = {
    kafka: 'KF',
    rabbitmq: 'RMQ',
    sqs: 'SQS',
    pubsub: 'PS',
    nats: 'NATS',
    websocket: 'WS',
    sse: 'SSE',
    graphql_subscription: 'GQL',
  }
  return map[protocol] ?? '?'
}

function truncateLabel(label: string, maxLen: number): string {
  return label.length > maxLen ? label.slice(0, maxLen - 1) + '…' : label
}
