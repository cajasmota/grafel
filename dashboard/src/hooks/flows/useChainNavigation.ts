import { useState, useCallback } from 'react'
import type { ProcessStep } from '@/types/api'

/**
 * Keyboard-driven navigation within a chain.
 * Returns the currently focused step index and handlers for ArrowUp/ArrowDown/Enter.
 */
export function useChainNavigation(
  steps: ProcessStep[] | undefined,
  onEnter?: (step: ProcessStep) => void,
) {
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null)

  const length = steps?.length ?? 0

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!steps || steps.length === 0) return

      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setFocusedIndex((prev) =>
          prev === null ? 0 : Math.min(prev + 1, length - 1),
        )
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setFocusedIndex((prev) =>
          prev === null ? length - 1 : Math.max(prev - 1, 0),
        )
      } else if (e.key === 'Enter' && focusedIndex !== null) {
        e.preventDefault()
        onEnter?.(steps[focusedIndex])
      } else if (e.key === 'Escape') {
        setFocusedIndex(null)
      }
    },
    [steps, focusedIndex, length, onEnter],
  )

  function focusStep(index: number) {
    setFocusedIndex(Math.max(0, Math.min(index, length - 1)))
  }

  return { focusedIndex, handleKeyDown, focusStep }
}
