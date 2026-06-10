/**
 * #4605 — guards against the cosmos.gl / regl `invalid texture shape` crash.
 *
 * The WebGL engine (@cosmos.gl/graph, regl under the hood) sizes its point and
 * cluster force textures from the point COUNT — typically as a square FBO of
 * side `ceil(sqrt(count))`. When the rendered graph is EMPTY (count === 0) that
 * side is 0, and regl's `texture2D` is handed a 0×0 shape → it throws
 * `(regl) invalid texture shape`, which bubbles up through `clusterTexture` /
 * `by.create` and trips the app error boundary.
 *
 * This happens on deep-links: `/g/<group>/graph?node=<id>` focuses the node's
 * ego sub-graph. When `<id>` is a SYNTHETIC / unresolved id (e.g. a Links-row
 * sink node `repo::sink:src/...::this.x.create@22`) it isn't present in the
 * rendered node set, so the ego filter yields ZERO nodes and an empty buffer is
 * fed to the engine.
 *
 * Two small, pure guards used by the canvas:
 *  - `isRenderableGraph` — should we feed cosmos at all? A 0-node graph is never
 *    renderable. (1 node / 0 edges is fine — a single point textures cleanly.)
 *  - `clampTextureDim` — never let a texture side be < 1; clamp + floor any
 *    non-finite / fractional / non-positive dimension to a safe minimum.
 */

/** Minimum side, in texels, for any GPU texture we let the engine allocate. */
export const MIN_TEXTURE_DIM = 1;

/**
 * True when the node set can be safely handed to the WebGL engine. An empty
 * graph (no points) yields a 0-sized texture and must NOT be pushed to cosmos —
 * the caller should render a graceful empty-state instead.
 *
 * A single isolated node (1 node, 0 edges) IS renderable: the point texture is
 * 1×1, the link buffer is empty, and the cluster path is skipped.
 */
export function isRenderableGraph(nodeCount: number, _edgeCount = 0): boolean {
  return Number.isFinite(nodeCount) && nodeCount >= 1;
}

/**
 * Clamp a texture dimension to a safe, integral minimum so regl never receives
 * a 0, negative, fractional, NaN or Infinity shape. Mirrors the engine's own
 * `ceil(sqrt(count))` sizing but with a hard floor of {@link MIN_TEXTURE_DIM}.
 */
export function clampTextureDim(dim: number): number {
  if (!Number.isFinite(dim)) return MIN_TEXTURE_DIM;
  const floored = Math.floor(dim);
  return floored < MIN_TEXTURE_DIM ? MIN_TEXTURE_DIM : floored;
}

/**
 * Square texture side for `count` points, with the safe floor applied. Useful
 * for any local texture/atlas allocation that mirrors the engine's sizing.
 */
export function textureSideFor(count: number): number {
  if (!Number.isFinite(count) || count <= 0) return MIN_TEXTURE_DIM;
  return clampTextureDim(Math.ceil(Math.sqrt(count)));
}
