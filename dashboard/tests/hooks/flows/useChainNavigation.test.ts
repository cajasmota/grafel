import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useChainNavigation } from '@/hooks/flows/useChainNavigation'
import type { ProcessStep } from '@/types/api'

const makeStep = (index: number): ProcessStep => ({
  entity_id: `id-${index}`,
  label: `Step ${index}`,
  source_file: 'file.py',
  start_line: index,
  repo: 'acme-api',
  step_index: index,
  edge_kind: 'CALLS',
})

const steps: ProcessStep[] = [makeStep(0), makeStep(1), makeStep(2)]

function makeKeyboardEvent(key: string): React.KeyboardEvent {
  return {
    key,
    preventDefault: vi.fn(),
  } as unknown as React.KeyboardEvent
}

describe('useChainNavigation', () => {
  it('starts with focusedIndex=null', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    expect(result.current.focusedIndex).toBeNull()
  })

  it('ArrowDown from null goes to index 0', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowDown')))
    expect(result.current.focusedIndex).toBe(0)
  })

  it('ArrowDown increments index', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowDown')))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowDown')))
    expect(result.current.focusedIndex).toBe(1)
  })

  it('ArrowDown clamps at last step', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.focusStep(2))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowDown')))
    expect(result.current.focusedIndex).toBe(2)
  })

  it('ArrowUp from null goes to last index', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowUp')))
    expect(result.current.focusedIndex).toBe(2)
  })

  it('ArrowUp decrements index', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.focusStep(2))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowUp')))
    expect(result.current.focusedIndex).toBe(1)
  })

  it('Enter calls onEnter with focused step', () => {
    const onEnter = vi.fn()
    const { result } = renderHook(() => useChainNavigation(steps, onEnter))
    act(() => result.current.focusStep(1))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('Enter')))
    expect(onEnter).toHaveBeenCalledWith(steps[1])
  })

  it('Escape resets to null', () => {
    const { result } = renderHook(() => useChainNavigation(steps))
    act(() => result.current.focusStep(1))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('Escape')))
    expect(result.current.focusedIndex).toBeNull()
  })

  it('handles undefined steps gracefully', () => {
    const { result } = renderHook(() => useChainNavigation(undefined))
    act(() => result.current.handleKeyDown(makeKeyboardEvent('ArrowDown')))
    expect(result.current.focusedIndex).toBeNull()
  })
})
