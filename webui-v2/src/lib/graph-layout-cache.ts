/* ============================================================
   lib/graph-layout-cache.ts — persist / restore settled graph positions.

   LESSON PORTED FROM v1: caching the settled Float32 positions in
   localStorage lets a return visit skip the explode/settle animation
   entirely and render the laid-out graph INSTANTLY.

   Key:   archigraph.v2.layout.<group>.<nodesetHash>
   Value: base64-encoded Float32Array of [x0, y0, x1, y1, ...]

   The node-set hash (FNV-1a over sorted node IDs) keys the cache so a graph
   whose nodes changed (re-index) misses and re-lays-out. A 2 MB guard avoids
   blowing the localStorage quota on huge graphs.
   ============================================================ */

const MAX_BYTES = 2 * 1024 * 1024;
const PREFIX = "archigraph.v2.layout";

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
  return `${PREFIX}.${group}.${hash}`;
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
    localStorage.setItem(layoutKey(group, nodeIds), encoded);
  } catch {
    /* ignore quota / private-mode */
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
  // Degenerate = the cloud has COLLAPSED toward a point — its span is tiny
  // relative to the node count. A healthy cosmos.gl settle spreads roughly
  // sqrt(n) × ~10 units of span (empirically ~584 for 3000 nodes); a contracted /
  // mid-collapse snapshot is many times smaller. We threshold well BELOW the
  // healthy span (sqrt(n) × 3, ≈164 for 3000) so a good spread is never rejected,
  // while a true collapse (all nodes piled together) still trips it. The floor
  // (40) catches the fully-collapsed case on tiny graphs.
  const minHealthySpan = Math.max(40, Math.sqrt(n) * 3);
  return span < minHealthySpan;
}

export function clearLayout(group: string, nodeIds: string[]): void {
  try {
    localStorage.removeItem(layoutKey(group, nodeIds));
  } catch {
    /* ignore */
  }
}
