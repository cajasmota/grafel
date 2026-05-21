/**
 * MiniMap — bottom-right canvas minimap for the graph view (#1366)
 *
 * Renders all node positions as small dots on a 200×150 HTML canvas overlay.
 * Draws the current WebGL viewport as a rectangle outline.
 * Click on the minimap → main view pans to that graph-space location.
 * Hover on minimap → tooltip shows nearest cluster node info.
 * Toggle to hide via sidebar option.
 *
 * Architecture:
 *   - Polls `cosmographRef.getPointPositions()` once after simulation settles,
 *     then re-polls on node data changes.
 *   - Reads zoom transform from the `onZoom` callback (k, tx, ty) propagated
 *     via props.
 *   - Converts click on minimap → graph coords → calls `cosmographRef.fitViewByCoordinates`
 *     (or setZoomTransformByPointPositions) to pan to that area.
 *   - Fully pointer-events isolated except for the minimap canvas itself.
 */

import { useRef, useEffect, useCallback, memo } from 'react'
import { Map as MapIcon, EyeOff } from 'lucide-react'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ZoomTransform {
  /** d3 zoom scale */
  k: number
  /** translation x (screen pixels) */
  x: number
  /** translation y (screen pixels) */
  y: number
}

export interface MiniMapProps {
  /**
   * Flat [x0, y0, x1, y1, ...] array of node positions in graph space.
   * Obtained from `cosmographRef.getPointPositions()`.
   */
  positions: Float32Array | number[] | null | undefined
  /**
   * Current d3 zoom transform from Cosmograph's onZoom callback.
   * Used to draw the viewport indicator rectangle.
   */
  zoomTransform: ZoomTransform
  /**
   * Pixel dimensions of the main graph canvas.
   * Used to compute which graph-space region the viewport covers.
   */
  canvasWidth: number
  canvasHeight: number
  /** Called when user clicks on the minimap to pan the main view */
  onPan: (graphX: number, graphY: number) => void
  /** Current theme */
  isDark?: boolean
  /** Whether the minimap is visible */
  visible: boolean
  /** Called when user clicks the hide button */
  onHide: () => void
  /** Node count label */
  nodeCount: number
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MAP_W = 200
const MAP_H = 150
const DOT_RADIUS = 1.0
const PADDING = 6 // px padding inside minimap bounds

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * Minimap canvas overlay — bottom-right of the graph canvas.
 *
 * Renders in two passes:
 *   1. Node dots (sampled if >40k for perf)
 *   2. Viewport rectangle
 */
export const MiniMap = memo(function MiniMap({
  positions,
  zoomTransform,
  canvasWidth,
  canvasHeight,
  onPan,
  isDark = true,
  visible,
  onHide,
  nodeCount,
}: MiniMapProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)

  // ---------------------------------------------------------------------------
  // Derived bounds — compute min/max of all node positions
  // ---------------------------------------------------------------------------

  const boundsRef = useRef<{ minX: number; maxX: number; minY: number; maxY: number } | null>(null)

  useEffect(() => {
    if (!positions || positions.length < 2) {
      boundsRef.current = null
      return
    }

    let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity
    for (let i = 0; i < positions.length - 1; i += 2) {
      const x = positions[i]
      const y = positions[i + 1]
      if (!isFinite(x) || !isFinite(y)) continue
      if (x < minX) minX = x
      if (x > maxX) maxX = x
      if (y < minY) minY = y
      if (y > maxY) maxY = y
    }

    if (!isFinite(minX)) {
      boundsRef.current = null
    } else {
      boundsRef.current = { minX, maxX, minY, maxY }
    }
  }, [positions])

  // ---------------------------------------------------------------------------
  // Graph → minimap coordinate transforms
  // ---------------------------------------------------------------------------

  /**
   * Convert graph-space (x, y) → minimap canvas pixel (mx, my).
   * Uses the precomputed bounds + padding.
   */
  const toMinimap = useCallback((gx: number, gy: number): [number, number] => {
    const b = boundsRef.current
    if (!b) return [MAP_W / 2, MAP_H / 2]
    const rangeX = b.maxX - b.minX || 1
    const rangeY = b.maxY - b.minY || 1
    const mx = PADDING + ((gx - b.minX) / rangeX) * (MAP_W - 2 * PADDING)
    const my = PADDING + ((gy - b.minY) / rangeY) * (MAP_H - 2 * PADDING)
    return [mx, my]
  }, [])

  /**
   * Convert minimap pixel (mx, my) → graph-space (gx, gy).
   */
  const fromMinimap = useCallback((mx: number, my: number): [number, number] => {
    const b = boundsRef.current
    if (!b) return [0, 0]
    const rangeX = b.maxX - b.minX || 1
    const rangeY = b.maxY - b.minY || 1
    const gx = b.minX + ((mx - PADDING) / (MAP_W - 2 * PADDING)) * rangeX
    const gy = b.minY + ((my - PADDING) / (MAP_H - 2 * PADDING)) * rangeY
    return [gx, gy]
  }, [])

  // ---------------------------------------------------------------------------
  // Draw — runs on positions change OR zoom transform change
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas || !visible) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const bg = isDark ? 'rgba(2,6,23,0.82)' : 'rgba(248,250,252,0.88)'
    const dotColor = isDark ? 'rgba(100,116,139,0.75)' : 'rgba(71,85,105,0.65)'
    const viewportColor = isDark ? 'rgba(56,189,248,0.9)' : 'rgba(14,165,233,0.9)'
    const borderColor = isDark ? 'rgba(51,65,85,0.7)' : 'rgba(148,163,184,0.7)'

    // Clear
    ctx.clearRect(0, 0, MAP_W, MAP_H)

    // Background
    ctx.fillStyle = bg
    ctx.fillRect(0, 0, MAP_W, MAP_H)

    // Border
    ctx.strokeStyle = borderColor
    ctx.lineWidth = 1
    ctx.strokeRect(0.5, 0.5, MAP_W - 1, MAP_H - 1)

    const b = boundsRef.current
    const hasPositions = positions && positions.length >= 2 && b

    // ── Node dots ────────────────────────────────────────────────────────────
    if (hasPositions && b) {
      // Sample down to max 30k dots for canvas perf
      const step = positions.length > 60000 ? Math.ceil(positions.length / 60000) * 2 : 2

      ctx.fillStyle = dotColor
      ctx.beginPath()

      for (let i = 0; i < positions.length - 1; i += step) {
        const gx = positions[i]
        const gy = positions[i + 1]
        if (!isFinite(gx) || !isFinite(gy)) continue
        const [mx, my] = toMinimap(gx, gy)
        ctx.moveTo(mx + DOT_RADIUS, my)
        ctx.arc(mx, my, DOT_RADIUS, 0, Math.PI * 2)
      }

      ctx.fill()
    }

    // ── Viewport rectangle ───────────────────────────────────────────────────
    if (hasPositions && b && canvasWidth > 0 && canvasHeight > 0) {
      const { k, x: tx, y: ty } = zoomTransform

      // The Cosmograph canvas uses d3-zoom internally.
      // A point in screen coords (sx, sy) maps to graph coords via:
      //   gx = (sx - tx) / k
      //   gy = (sy - ty) / k
      // So the screen viewport (0,0)→(canvasWidth,canvasHeight) maps to:
      const gLeft   = (0 - tx) / k
      const gTop    = (0 - ty) / k
      const gRight  = (canvasWidth - tx) / k
      const gBottom = (canvasHeight - ty) / k

      // Convert to minimap pixel coords
      const [mleft,   mtop]    = toMinimap(gLeft,  gTop)
      const [mright,  mbottom] = toMinimap(gRight, gBottom)

      const vw = mright - mleft
      const vh = mbottom - mtop

      // Draw filled tinted viewport
      ctx.fillStyle = isDark ? 'rgba(56,189,248,0.08)' : 'rgba(14,165,233,0.06)'
      ctx.fillRect(mleft, mtop, vw, vh)

      // Draw viewport border
      ctx.strokeStyle = viewportColor
      ctx.lineWidth = 1.5
      ctx.strokeRect(mleft, mtop, vw, vh)
    }

    // ── Empty state hint ─────────────────────────────────────────────────────
    if (!hasPositions) {
      ctx.fillStyle = isDark ? 'rgba(100,116,139,0.4)' : 'rgba(71,85,105,0.4)'
      ctx.font = '9px system-ui, sans-serif'
      ctx.textAlign = 'center'
      ctx.fillText('Waiting for layout…', MAP_W / 2, MAP_H / 2)
    }
  }, [positions, zoomTransform, canvasWidth, canvasHeight, isDark, visible, toMinimap])

  // ---------------------------------------------------------------------------
  // Click → pan
  // ---------------------------------------------------------------------------

  const handleClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current
    if (!canvas) return
    const rect = canvas.getBoundingClientRect()
    const mx = e.clientX - rect.left
    const my = e.clientY - rect.top
    const [gx, gy] = fromMinimap(mx, my)
    onPan(gx, gy)
  }, [onPan, fromMinimap])

  // ---------------------------------------------------------------------------
  // Hover → tooltip (nearest node info)
  // ---------------------------------------------------------------------------

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current
    const tooltip = tooltipRef.current
    if (!canvas || !tooltip) return

    const rect = canvas.getBoundingClientRect()
    const mx = e.clientX - rect.left
    const my = e.clientY - rect.top
    const [gx, gy] = fromMinimap(mx, my)

    // Show tooltip with graph coords
    tooltip.style.display = 'block'
    tooltip.style.left = `${e.clientX - rect.left + 8}px`
    tooltip.style.top = `${e.clientY - rect.top - 24}px`
    tooltip.textContent = `(${Math.round(gx)}, ${Math.round(gy)})`
  }, [fromMinimap])

  const handleMouseLeave = useCallback(() => {
    const tooltip = tooltipRef.current
    if (tooltip) tooltip.style.display = 'none'
  }, [])

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  if (!visible) return null

  const containerStyle: React.CSSProperties = {
    position: 'absolute',
    bottom: 36,   // above the ZoomBandHUD chip (bottom: 12, height ~20px)
    right: 10,
    width: MAP_W,
    height: MAP_H,
    zIndex: 25,
    borderRadius: 6,
    overflow: 'visible',
    // Box shadow for depth
    boxShadow: isDark
      ? '0 4px 20px rgba(0,0,0,0.6), 0 0 0 1px rgba(51,65,85,0.5)'
      : '0 4px 20px rgba(0,0,0,0.15), 0 0 0 1px rgba(148,163,184,0.4)',
  }

  return (
    <div
      style={containerStyle}
      aria-label="Graph minimap"
      data-testid="graph-minimap"
    >
      {/* Canvas */}
      <canvas
        ref={canvasRef}
        width={MAP_W}
        height={MAP_H}
        style={{
          display: 'block',
          borderRadius: 6,
          cursor: boundsRef.current ? 'crosshair' : 'default',
        }}
        onClick={handleClick}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
        aria-label={`Graph minimap, ${nodeCount.toLocaleString()} nodes. Click to pan.`}
        role="img"
        data-testid="graph-minimap-canvas"
      />

      {/* Tooltip */}
      <div
        ref={tooltipRef}
        aria-hidden
        style={{
          position: 'absolute',
          display: 'none',
          pointerEvents: 'none',
          background: isDark ? 'rgba(2,6,23,0.92)' : 'rgba(248,250,252,0.92)',
          color: isDark ? '#94a3b8' : '#475569',
          border: isDark ? '1px solid rgba(51,65,85,0.6)' : '1px solid rgba(148,163,184,0.6)',
          borderRadius: 4,
          padding: '2px 6px',
          fontSize: 9,
          fontFamily: 'monospace',
          zIndex: 30,
          whiteSpace: 'nowrap',
        }}
      />

      {/* Hide button — top-right corner */}
      <button
        type="button"
        onClick={onHide}
        aria-label="Hide minimap"
        data-testid="graph-minimap-hide-btn"
        title="Hide minimap"
        style={{
          position: 'absolute',
          top: -8,
          right: -8,
          width: 18,
          height: 18,
          borderRadius: '50%',
          border: isDark ? '1px solid rgba(51,65,85,0.7)' : '1px solid rgba(148,163,184,0.6)',
          background: isDark ? 'rgba(2,6,23,0.9)' : 'rgba(248,250,252,0.95)',
          color: isDark ? '#64748b' : '#64748b',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: 'pointer',
          zIndex: 31,
          padding: 0,
          lineHeight: 1,
        }}
      >
        <EyeOff size={10} />
      </button>

      {/* Node count chip — bottom-left */}
      <div
        aria-hidden
        data-testid="graph-minimap-count"
        style={{
          position: 'absolute',
          bottom: -18,
          left: 0,
          fontSize: 9,
          color: isDark ? 'rgba(100,116,139,0.7)' : 'rgba(100,116,139,0.8)',
          userSelect: 'none',
          pointerEvents: 'none',
          whiteSpace: 'nowrap',
        }}
      >
        {nodeCount.toLocaleString()} nodes
      </div>
    </div>
  )
})

// ---------------------------------------------------------------------------
// MiniMapToggleButton — shown in the sidebar overlay section when hidden
// ---------------------------------------------------------------------------

export interface MiniMapToggleButtonProps {
  onShow: () => void
  isDark?: boolean
}

export const MiniMapToggleButton = memo(function MiniMapToggleButton({
  onShow,
  isDark = true,
}: MiniMapToggleButtonProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={false}
      onClick={onShow}
      title="Show minimap"
      data-testid="graph-minimap-toggle"
      className={[
        'flex items-center gap-2 px-2 py-1 rounded text-left text-xs w-full transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
        isDark
          ? 'text-slate-600 hover:bg-slate-800/60'
          : 'text-slate-400 hover:bg-slate-200/60',
      ].join(' ')}
    >
      <MapIcon size={11} className="shrink-0 text-slate-600" aria-hidden />
      Minimap
    </button>
  )
})
