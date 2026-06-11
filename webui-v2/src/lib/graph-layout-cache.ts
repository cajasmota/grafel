/* ============================================================
   lib/graph-layout-cache.ts — persist / restore settled graph positions.

   LESSON PORTED FROM v1: caching the settled Float32 positions in
   localStorage lets a return visit skip the explode/settle animation
   entirely and render the laid-out graph INSTANTLY.

   Key:   archigraph.v2.layout.<layoutVersion>.<group>.<nodesetHash>
   Value: base64-encoded Float32Array of [x0, y0, x1, y1, ...]

   The node-set hash (FNV-1a over sorted node IDs) keys the cache so a graph
   whose nodes changed (re-index) misses and re-lays-out. A 2 MB guard avoids
   blowing the localStorage quota on huge graphs.

   Fix #1581: the cache key is ALSO scoped by a LAYOUT_VERSION. The settled
   positions are a product of the simulation / force defaults; when we ship new
   force defaults (#1566/#1569/#1586/…) the OLD cached positions are stale — a
   reload restored a layout produced by forces that no longer exist (the
   over-contracted blob), while Reset re-ran the sim with the new forces and
   produced the good spread. Bumping LAYOUT_VERSION whenever the layout-producing
   defaults change makes every key under the old version a guaranteed MISS, so a
   ship of new forces always re-settles. (Keep this in lock-step with the store's
   DEFAULTS_VERSION — bump both when DEFAULT_SIMULATION changes.)
   ============================================================ */

const MAX_BYTES = 2 * 1024 * 1024;
const PREFIX = "archigraph.v2.layout";

/**
 * Fix #1581: version stamp baked into every layout-cache key. Bump this whenever
 * a change can alter the SETTLED GEOMETRY for the same node set — i.e. any change
 * to the simulation / force defaults (DEFAULT_SIMULATION), the cluster-seeding, or
 * the settle routine. Old-version entries become unreachable keys (a miss), so the
 * graph re-settles with the current forces on next load instead of restoring a
 * layout baked by retired forces. Tracks the store's DEFAULTS_VERSION (=4 at the
 * time of #1581).
 *
 * Fix #2107: bump to 5 — DEFAULTS_VERSION was already bumped to 5 in use-graph-store
 * (#1607 sizing model overhaul) but LAYOUT_VERSION was left at 4. The mismatch meant
 * the store discarded stale tuning but the layout cache STILL LOADED old positions
 * (cached under the v4 key) produced by the retired forces. Since those positions
 * were spread enough to pass isDegenerateLayout they were accepted and the graph
 * rendered collapsed on every reload. Keeping this in lock-step with DEFAULTS_VERSION
 * guarantees all v4 caches are a guaranteed miss; first load re-settles with current
 * forces (== Reset). Reload === Reset by construction.
 *
 * Fix #4492: bump to 6 — #4470 (low-degree leaf ANCHORING — seeding any rendered-
 * degree-≤1 node on top of its single neighbor instead of in its group-center blob)
 * changed the INITIAL-POSITION SEEDING and therefore the SETTLED GEOMETRY for the
 * same node set, but neither version stamp was bumped. So a returning user's v5
 * cached layout (baked WITHOUT the anchoring) still passed isLayoutHealthy and was
 * PINNED STATIC on load (the doSettle cache path) — the force sim never ran, so the
 * graph showed the old un-anchored layout and only "exploded" into the new spread
 * after a manual Reset (which routes through kickFreshSettle). Bumping makes every
 * v5 layout a guaranteed miss → first load runs the unified fresh settle. Reload ===
 * Reset by construction. (Kept in lock-step with DEFAULTS_VERSION = 6.)
 *
 * Fix #4852: bump to 7 — kept in lock-step with DEFAULTS_VERSION = 7, which was
 * bumped to ship the retuned DEFAULT_RENDER.linkOpacity. No layout-producing force
 * changed (this is a pure render/opacity tweak), so the only effect of this bump is
 * a harmless one-time re-settle on next load; the lock-step is maintained purely to
 * honor the documented protocol (the two stamps must move together).
 */
export const LAYOUT_VERSION = 7;

function fnv1a32(s: string): string {
  let h = 0x811c9dc5 >>> 0;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return String(h);
}

function layoutKey(group: string, nodeIds: string[]): string {
  const hash = fnv1a32([...nodeIds].sort().join(","));
  // Fix #1581: scope by LAYOUT_VERSION so a defaults bump invalidates old caches.
  return `${PREFIX}.${LAYOUT_VERSION}.${group}.${hash}`;
}

function float32ToBase64(arr: Float32Array): string {
  const bytes = new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength);
  let s = "";
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}

function base64ToFloat32(b64: string): Float32Array | null {
  try {
    const s = atob(b64);
    const bytes = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) bytes[i] = s.charCodeAt(i);
    return new Float32Array(bytes.buffer);
  } catch {
    return null;
  }
}

export interface LayoutCacheEntry {
  positions: Float32Array;
}

export function saveLayout(group: string, nodeIds: string[], positions: Float32Array): void {
  try {
    const encoded = float32ToBase64(positions);
    if (encoded.length > MAX_BYTES) return;
    // Fix #1581: drop any layout entries from an OLDER version for this group +
    // node set so stale blobs can never be read back and don't accumulate in
    // localStorage. (We can only namespace-scan the keys we know how to rebuild.)
    pruneOldVersions(group, nodeIds);
    localStorage.setItem(layoutKey(group, nodeIds), encoded);
  } catch {
    /* ignore quota / private-mode */
  }
}

/**
 * Fix #1581: remove layout-cache entries written under a PREVIOUS LAYOUT_VERSION
 * for this exact group + node set, so a reload can never fall back to a layout
 * baked by retired forces and localStorage doesn't grow unbounded across ships.
 */
function pruneOldVersions(group: string, nodeIds: string[]): void {
  try {
    const hash = fnv1a32([...nodeIds].sort().join(","));
    for (let v = 0; v < LAYOUT_VERSION; v++) {
      localStorage.removeItem(`${PREFIX}.${v}.${group}.${hash}`);
    }
  } catch {
    /* ignore */
  }
}

export function loadLayout(group: string, nodeIds: string[]): LayoutCacheEntry | null {
  try {
    const encoded = localStorage.getItem(layoutKey(group, nodeIds));
    if (!encoded) return null;
    const positions = base64ToFloat32(encoded);
    if (!positions || positions.length !== nodeIds.length * 2) {
      localStorage.removeItem(layoutKey(group, nodeIds));
      return null;
    }
    return { positions };
  } catch {
    return null;
  }
}

/**
 * Fix #1567-2: detect a DEGENERATE (over-contracted / collapsed) cached layout.
 * The bug: doSettle's cap timer can fire while the sim is still mid-collapse, so
 * the cache snapshots a contracted blob; reloading then renders that blob (Reset
 * re-runs the sim → good spread). We treat a layout as degenerate when its
 * bounding box is tiny relative to the node count — i.e. the nodes are all piled
 * near one point instead of spread across the canvas. A well-settled layout for N
 * nodes spans roughly sqrt(N)*spacing; if the actual span is far below that the
 * cache is bad and the caller should re-settle (skip the cache) instead.
 */
export function isDegenerateLayout(positions: Float32Array): boolean {
  const n = positions.length / 2;
  if (n < 2) return false;
  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  for (let i = 0; i < n; i++) {
    const x = positions[i * 2];
    const y = positions[i * 2 + 1];
    if (!Number.isFinite(x) || !Number.isFinite(y)) return true; // garbage → degenerate
    if (x < minX) minX = x;
    if (x > maxX) maxX = x;
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
  }
  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const span = Math.max(spanX, spanY);
  // Fix #1581: reject both a COLLAPSED layout (all nodes piled near a point) AND
  // an OVER-CONTRACTED one (a tight ball / mushed clusters — the bug). A healthy
  // cosmos.gl settle under the current forces spreads roughly sqrt(n) × ~10 units
  // (empirically ~584 for 3000 nodes); the over-contracted reload blob is several
  // times smaller. The earlier threshold (sqrt(n) × 3) only caught a near-total
  // collapse, so the tight-ball reload slipped through and looked wrong vs Reset.
  // Raise the healthy floor to sqrt(n) × 6 (≈329 for 3000) — still comfortably
  // below a real spread so a good layout is never rejected, but now a contracted
  // ball trips it and the caller re-settles (matching Reset). The absolute floor
  // (60) catches the fully-collapsed case on tiny graphs.
  const minHealthySpan = Math.max(60, Math.sqrt(n) * 6);
  if (span < minHealthySpan) return true;

  // Fix #4844: the span check alone is a MAX-DIMENSION test, so it is fooled by a
  // dense HAIRBALL on a large graph. On upvate-v3 (~14k rendered nodes) a collapsed
  // cache can still span a couple thousand units along its WIDEST axis — clearing
  // minHealthySpan (sqrt(14233)×6 ≈ 716) — while packing all 14k nodes into a small
  // box, rendering as the reported hairball. Reload PINNED that cache (the doSettle
  // cache path, no sim) while Reset re-ran kickFreshSettle and spread it, so reload
  // ≠ Reset. Add a DENSITY (area-per-node) gate so a too-dense bbox also trips as
  // degenerate. We compute the EXPECTED span of a healthy settle from the same law
  // the span floor uses (a real ~584-span/3000-node settle ≈ sqrt(n)×~10), square it
  // for the expected area, and require the cached layout to occupy at least a small
  // FRACTION of that area per node. A genuinely spread layout sits well above this
  // fraction (a good spread is at/above the expected area), while a hairball — many
  // nodes mushed into a small box — falls far below it, so it re-settles to match
  // Reset. The fraction is deliberately conservative (a real spread is never
  // rejected) and the gate only engages on non-trivially sized graphs; small graphs
  // are already covered by the span floor above.
  const area = spanX * spanY;
  // Expected healthy bbox area for n nodes ≈ (sqrt(n) × 10)² = n × 100, i.e. ~100 sq
  // units of bbox per node at the reference settle. Require ≥ 25% of that (~25 sq
  // units/node) — far below any real spread, but a dense hairball (area/node in the
  // single digits to low tens) trips it.
  const HEALTHY_AREA_PER_NODE = 100;
  const MIN_AREA_FRACTION = 0.25;
  if (n >= 500 && area < n * HEALTHY_AREA_PER_NODE * MIN_AREA_FRACTION) return true;

  return false;
}

/**
 * Fix #1581: convenience predicate — a cached layout is GOOD (reusable on load)
 * when every coordinate is finite AND the bounding-box spread is healthy for the
 * node count (not collapsed, not an over-contracted ball). Callers reuse the cache
 * only when this passes; otherwise they re-settle (== Reset).
 */
export function isLayoutHealthy(positions: Float32Array, expectedLen: number): boolean {
  if (positions.length !== expectedLen) return false;
  for (let i = 0; i < positions.length; i++) {
    if (!Number.isFinite(positions[i])) return false;
  }
  return !isDegenerateLayout(positions);
}

export function clearLayout(group: string, nodeIds: string[]): void {
  try {
    localStorage.removeItem(layoutKey(group, nodeIds));
  } catch {
    /* ignore */
  }
}
