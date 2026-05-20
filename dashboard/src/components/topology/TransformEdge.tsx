interface TransformEdgeProps {
  fromId: string
  toId: string
  fromX: number
  fromY: number
  toX: number
  toY: number
  isHighlighted?: boolean
}

/**
 * Dashed topic→topic TRANSFORMS edge.
 * Curved path to visually distinguish from producer/consumer spokes.
 * Amber color reserved for transform edges system-wide.
 */
export function TransformEdge({
  fromId,
  toId,
  fromX,
  fromY,
  toX,
  toY,
  isHighlighted = false,
}: TransformEdgeProps) {
  const markerId = `arrow-t-${fromId.replace(/[^a-z0-9]/gi, '_')}-${toId.replace(/[^a-z0-9]/gi, '_')}`
  const color = '#fbbf24' // amber-400 — transforms are always amber

  // Midpoint control for a gentle curve
  const mx = (fromX + toX) / 2
  const my = (fromY + toY) / 2 - 40

  const opacity = isHighlighted ? 1 : 0.7
  const strokeWidth = isHighlighted ? 2 : 1.5

  return (
    <g aria-label="TRANSFORMS edge" role="img">
      <defs>
        <marker
          id={markerId}
          markerWidth="6"
          markerHeight="6"
          refX="5"
          refY="3"
          orient="auto"
        >
          <path d="M0,0 L0,6 L6,3 Z" fill={color} opacity={opacity} />
        </marker>
      </defs>
      <path
        d={`M${fromX},${fromY} Q${mx},${my} ${toX},${toY}`}
        fill="none"
        stroke={color}
        strokeWidth={strokeWidth}
        strokeDasharray="6,3"
        opacity={opacity}
        markerEnd={`url(#${markerId})`}
      />
      {/* "transforms" label at midpoint */}
      <text
        x={mx}
        y={my - 6}
        textAnchor="middle"
        fontSize={9}
        fill={color}
        opacity={opacity}
        fontFamily="ui-monospace, monospace"
        aria-hidden
      >
        TRANSFORMS
      </text>
    </g>
  )
}
