/* ============================================================
   edge-geometry.ts — parametric position along a polyline edge.

   The flow DAG edges are simple polylines. To position a comet at
   progress p ∈ [0, 1] along an edge, we compute the cumulative arc
   length of each segment and interpolate. Zero deps.
   ============================================================ */

export type Pt = { x: number; y: number };

export function polyLength(points: Pt[]): number {
  let total = 0;
  for (let i = 1; i < points.length; i++) {
    const dx = points[i].x - points[i - 1].x;
    const dy = points[i].y - points[i - 1].y;
    total += Math.hypot(dx, dy);
  }
  return total;
}

/** Returns the point at fractional progress p along the polyline. */
export function pointAt(points: Pt[], p: number): Pt {
  if (points.length === 0) return { x: 0, y: 0 };
  if (points.length === 1) return points[0];
  const clamped = Math.max(0, Math.min(1, p));
  const total = polyLength(points);
  if (total === 0) return points[0];
  const target = total * clamped;
  let acc = 0;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    const seg = Math.hypot(b.x - a.x, b.y - a.y);
    if (acc + seg >= target) {
      const t = seg === 0 ? 0 : (target - acc) / seg;
      return { x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t };
    }
    acc += seg;
  }
  return points[points.length - 1];
}
