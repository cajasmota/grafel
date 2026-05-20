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
  /** Whether the force simulation is currently running */
  simulationRunning: boolean

  // Actions
  setGraphRef: (ref: CosmographInstance | null) => void
  setZoomLevel: (z: number) => void
  setHoveredNode: (id: string | null) => void
  zoomToNode: (nodeId: string) => void
  resetView: () => void
  /** Fit all visible nodes into the viewport (smooth pan+zoom) */
  fitView: () => void
  /** Reset zoom level to 1:1 then fit view */
  resetZoom: () => void
  /** Pause the force simulation */
  pauseSimulation: () => void
  /** Resume the force simulation */
  resumeSimulation: () => void
  /** Toggle simulation pause/resume */
  toggleSimulation: () => void
}

export const useGraphCameraStore = create<GraphCameraState>((set, get) => ({
  graphRef: null,
  zoomLevel: 1.0,
  hoveredNodeId: null,
  simulationRunning: true,

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

  fitView: () => {
    const { graphRef } = get()
    if (!graphRef) return
    graphRef.fitView(500)
  },

  resetZoom: () => {
    const { graphRef } = get()
    if (!graphRef) return
    // Set zoom to 1:1 first, then fit view so all nodes are visible
    graphRef.setZoomLevel(1, 300)
    setTimeout(() => graphRef.fitView(400), 350)
  },

  pauseSimulation: () => {
    const { graphRef } = get()
    if (!graphRef) return
    graphRef.pause()
    set({ simulationRunning: false })
  },

  resumeSimulation: () => {
    const { graphRef } = get()
    if (!graphRef) return
    graphRef.unpause()
    set({ simulationRunning: true })
  },

  toggleSimulation: () => {
    const { simulationRunning, pauseSimulation, resumeSimulation } = get()
    if (simulationRunning) {
      pauseSimulation()
    } else {
      resumeSimulation()
    }
  },
}))

/** Convenience selector hooks */
export const useGraphRef = () => useGraphCameraStore((s) => s.graphRef)
export const useZoomLevel = () => useGraphCameraStore((s) => s.zoomLevel)
export const useHoveredNodeId = () => useGraphCameraStore((s) => s.hoveredNodeId)
export const useSimulationRunning = () => useGraphCameraStore((s) => s.simulationRunning)
