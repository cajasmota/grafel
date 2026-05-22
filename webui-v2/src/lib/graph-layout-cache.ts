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
 */
export const LAYOUT_VERSION = 4;

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
  return span < minHealthySpan;
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
