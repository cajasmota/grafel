import { repoColor } from '@/lib/colors'

interface ConsumerSpokeProps {
  consumerId: string
  consumerLabel: string
  consumerRepo: string
  /** SVG x,y of the topic node center (source) */
  tx: number
  ty: number
  /** SVG x,y of the consumer entity endpoint (target) */
  cx: number
  cy: number
  isHighlighted?: boolean
}

/**
 * Directed line from topic hub → consumer entity.
 * Color encodes consumer repo (stable per-slug). Arrow tip at consumer end.
 */
export function ConsumerSpoke({
  consumerId,
  consumerLabel,
  consumerRepo,
  tx,
  ty,
  cx,
  cy,
  isHighlighted = false,
}: ConsumerSpokeProps) {
  const color = repoColor(consumerRepo)
  const markerId = `arrow-c-${consumerId.replace(/[^a-z0-9]/gi, '_')}`

  const opacity = isHighlighted ? 1 : 0.55
  const strokeWidth = isHighlighted ? 1.8 : 1.2

  return (
    <g aria-label={`Consumer: topic → ${consumerLabel}`} role="img">
      <defs>
        <marker
          id={markerId}
          markerWidth="6"
          markerHeight="6"
          refX="5"
          refY="3"
          orient="auto"
        >
          <path d="M0,0 L0,6 L6,3 Z" fill={color.dot} opacity={opacity} />
        </marker>
      </defs>
      <line
        x1={tx}
        y1={ty}
        x2={cx}
        y2={cy}
        stroke={color.dot}
        strokeWidth={strokeWidth}
        opacity={opacity}
        markerEnd={`url(#${markerId})`}
      />
    </g>
  )
}
