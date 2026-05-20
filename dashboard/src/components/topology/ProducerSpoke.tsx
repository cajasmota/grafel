import { repoColor } from '@/lib/colors'

interface ProducerSpokeProps {
  /** ID of the producer entity (source of arrow) */
  producerId: string
  /** Label of the producer entity */
  producerLabel: string
  /** Repo of the producer entity — drives spoke color */
  producerRepo: string
  /** SVG x,y of the producer endpoint (hub periphery) */
  px: number
  py: number
  /** SVG x,y of the topic node center */
  tx: number
  ty: number
  isHighlighted?: boolean
}

/**
 * Directed line from producer entity → topic hub.
 * Color encodes repo (stable per-slug). Arrow tip at topic end.
 */
export function ProducerSpoke({
  producerId,
  producerLabel,
  producerRepo,
  px,
  py,
  tx,
  ty,
  isHighlighted = false,
}: ProducerSpokeProps) {
  const color = repoColor(producerRepo)
  // Arrow-head marker id must be unique enough to avoid cross-component collision
  const markerId = `arrow-p-${producerId.replace(/[^a-z0-9]/gi, '_')}`

  const opacity = isHighlighted ? 1 : 0.55
  const strokeWidth = isHighlighted ? 1.8 : 1.2

  return (
    <g aria-label={`Producer: ${producerLabel} → topic`} role="img">
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
        x1={px}
        y1={py}
        x2={tx}
        y2={ty}
        stroke={color.dot}
        strokeWidth={strokeWidth}
        strokeDasharray={isHighlighted ? undefined : '4,2'}
        opacity={opacity}
        markerEnd={`url(#${markerId})`}
      />
    </g>
  )
}
