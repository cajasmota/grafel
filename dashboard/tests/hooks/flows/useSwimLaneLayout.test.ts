import { describe, it, expect } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useSwimLaneLayout } from '@/hooks/flows/useSwimLaneLayout'
import type { ProcessStep } from '@/types/api'

const steps: ProcessStep[] = [
  { entity_id: 'fe::fetchUsers', label: 'fetchUsers', source_file: 'src/api.ts', start_line: 1, repo: 'acme-frontend', step_index: 0, edge_kind: 'ENTRY_POINT_OF' },
  { entity_id: 'fe::apiClient', label: 'apiClient.get', source_file: 'src/client.ts', start_line: 10, repo: 'acme-frontend', step_index: 1, edge_kind: 'CALLS' },
  { entity_id: 'api::UserListView', label: 'UserListView', source_file: 'views.py', start_line: 5, repo: 'acme-api', step_index: 2, edge_kind: 'FETCHES' },
  { entity_id: 'api::user_table', label: 'user_table', source_file: 'models.py', start_line: 1, repo: 'acme-api', step_index: 3, edge_kind: 'ACCESSES_TABLE' },
]

describe('useSwimLaneLayout', () => {
  it('returns empty array for undefined steps', () => {
    const { result } = renderHook(() => useSwimLaneLayout(undefined))
    expect(result.current).toEqual([])
  })

  it('returns empty array for empty steps', () => {
    const { result } = renderHook(() => useSwimLaneLayout([]))
    expect(result.current).toEqual([])
  })

  it('groups steps by repo in appearance order', () => {
    const { result } = renderHook(() => useSwimLaneLayout(steps))
    expect(result.current).toHaveLength(2)
    expect(result.current[0].repo).toBe('acme-frontend')
    expect(result.current[1].repo).toBe('acme-api')
  })

  it('assigns lane indices sequentially', () => {
    const { result } = renderHook(() => useSwimLaneLayout(steps))
    expect(result.current[0].laneIndex).toBe(0)
    expect(result.current[1].laneIndex).toBe(1)
  })

  it('puts correct steps in each lane', () => {
    const { result } = renderHook(() => useSwimLaneLayout(steps))
    const frontendLane = result.current[0]
    expect(frontendLane.steps).toHaveLength(2)
    expect(frontendLane.steps[0].entity_id).toBe('fe::fetchUsers')

    const apiLane = result.current[1]
    expect(apiLane.steps).toHaveLength(2)
    expect(apiLane.steps[0].entity_id).toBe('api::UserListView')
  })

  it('handles single-repo chain (one lane)', () => {
    const intraRepo = steps.map((s) => ({ ...s, repo: 'acme-api' }))
    const { result } = renderHook(() => useSwimLaneLayout(intraRepo))
    expect(result.current).toHaveLength(1)
    expect(result.current[0].laneIndex).toBe(0)
    expect(result.current[0].steps).toHaveLength(4)
  })
})
