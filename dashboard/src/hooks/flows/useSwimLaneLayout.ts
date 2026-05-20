import { useMemo } from 'react'
import type { ProcessStep, SwimLaneEntry } from '@/types/api'

/**
 * Pure derivation: groups chain steps by repo and assigns lane indices.
 * Lane order = order of first appearance in the chain (entry-point repo is lane 0).
 * Returns a stable SwimLaneEntry[] sorted by laneIndex.
 */
export function useSwimLaneLayout(steps: ProcessStep[] | undefined): SwimLaneEntry[] {
  return useMemo(() => {
    if (!steps || steps.length === 0) return []

    const laneMap = new Map<string, { laneIndex: number; steps: ProcessStep[] }>()
    let nextIndex = 0

    for (const step of steps) {
      if (!laneMap.has(step.repo)) {
        laneMap.set(step.repo, { laneIndex: nextIndex++, steps: [] })
      }
      laneMap.get(step.repo)!.steps.push(step)
    }

    const result: SwimLaneEntry[] = []
    for (const [repo, { laneIndex, steps: laneSteps }] of laneMap) {
      result.push({ repo, steps: laneSteps, laneIndex })
    }

    return result.sort((a, b) => a.laneIndex - b.laneIndex)
  }, [steps])
}
