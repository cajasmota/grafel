import { create } from 'zustand'

/**
 * Zustand slice for the graph camera / Cosmograph instance.
 * High-frequency state (zoom, hover) that should NOT live in URL params.
 *
 * The `graphRef` is set by <GraphCanvas> once the Cosmograph instance mounts,
 * allowing toolbar / inspector to call imperative camera methods.
 *
 * #1023: migrated from 3d-force-graph to Cosmograph. The imperative API is now:
 *   - fitView(duration?) → replaces zoomToFit()
 *   - getPointIndicesByIds([id]) + zoomToPoint(index, duration) → replaces cameraPosition()
 */

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type CosmographInstance = any  // typed via @cosmograph/react CosmographRef

interface GraphCameraState {
  graphRef: CosmographInstance | null
  zoomLevel: number
  hoveredNodeId: string | null

  // Actions
  setGraphRef: (ref: CosmographInstance | null) => void
  setZoomLevel: (z: number) => void
  setHoveredNode: (id: string | null) => void
  zoomToNode: (nodeId: string) => void
  resetView: () => void
}

export const useGraphCameraStore = create<GraphCameraState>((set, get) => ({
  graphRef: null,
  zoomLevel: 1.0,
  hoveredNodeId: null,

  setGraphRef: (ref) => set({ graphRef: ref }),
  setZoomLevel: (z) => set({ zoomLevel: z }),
  setHoveredNode: (id) => set({ hoveredNodeId: id }),

  zoomToNode: async (nodeId) => {
    const { graphRef } = get()
    if (!graphRef) return
    // Cosmograph: resolve id → index, then zoom to that point
    const indices: number[] | undefined = await graphRef.getPointIndicesByIds([nodeId])
    if (indices && indices.length > 0) {
      graphRef.zoomToPoint(indices[0], 800)
    }
  },

  resetView: () => {
    const { graphRef } = get()
    if (!graphRef) return
    graphRef.fitView(600)
  },
}))

/** Convenience selector hooks */
export const useGraphRef = () => useGraphCameraStore((s) => s.graphRef)
export const useZoomLevel = () => useGraphCameraStore((s) => s.zoomLevel)
export const useHoveredNodeId = () => useGraphCameraStore((s) => s.hoveredNodeId)
