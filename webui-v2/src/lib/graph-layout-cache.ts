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

export function clearLayout(group: string, nodeIds: string[]): void {
  try {
    localStorage.removeItem(layoutKey(group, nodeIds));
  } catch {
    /* ignore */
  }
}
