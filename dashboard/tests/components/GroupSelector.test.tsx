import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { GroupSelector } from '@/components/layout/GroupSelector'
import type { GroupMeta } from '@/types/api'

const mockGroups: GroupMeta[] = [
  {
    id: 'fixture-a',
    display_name: 'Fixture A',
    repos: [],
    entity_count: 6916,
    indexed_at: '2026-05-20T10:05:00Z',
    bug_rate: 3,   // healthy — green
  },
  {
    id: 'fixture-b',
    display_name: 'Fixture B',
    repos: [],
    entity_count: 4557,
    indexed_at: '2026-05-20T09:05:00Z',
    bug_rate: 10,  // degraded — amber
  },
]

function renderWithRouter(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/:surface/:group"
          element={<GroupSelector groups={mockGroups} />}
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('GroupSelector', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders trigger with active group name', () => {
    renderWithRouter('/graph/fixture-a')
    expect(screen.getByTestId('group-selector-label').textContent).toBe('Fixture A')
  })

  it('opens panel when trigger is clicked', () => {
    renderWithRouter('/graph/fixture-a')
    expect(screen.queryByTestId('group-selector-panel')).toBeNull()
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    expect(screen.getByTestId('group-selector-panel')).toBeDefined()
  })

  it('renders all groups inside open panel', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    // 'Fixture A' appears in both the trigger label and the list row
    expect(screen.getAllByText('Fixture A').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Fixture B')).toBeDefined()
  })

  it('marks the active group as selected', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    const activeOption = screen.getAllByRole('option').find(
      (o) => o.getAttribute('aria-selected') === 'true',
    )
    expect(activeOption?.textContent).toContain('Fixture A')
  })

  it('filters groups by search query', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    const input = screen.getByTestId('group-selector-search')
    fireEvent.change(input, { target: { value: 'B' } })
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toContain('Fixture B')
  })

  it('renders health status dots for all groups', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    // Each group option row should have a health dot with aria-label starting with "Health:"
    const dots = screen.getAllByRole('option').flatMap(
      (o) => Array.from(o.querySelectorAll('[aria-label^="Health:"]')),
    )
    expect(dots.length).toBeGreaterThanOrEqual(2)
  })

  it('shows correct health tooltip on trigger dot', () => {
    renderWithRouter('/graph/fixture-a')
    const triggerDot = screen.getByTestId('group-selector-health-dot')
    // fixture-a has bug_rate=3 → healthy, fidelity 97%
    expect(triggerDot.getAttribute('title')).toContain('healthy')
    expect(triggerDot.getAttribute('title')).toContain('97')
  })

  it('shows "No groups match" when filter has no results', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    const input = screen.getByTestId('group-selector-search')
    fireEvent.change(input, { target: { value: 'zzz' } })
    expect(screen.getByText('No groups match')).toBeDefined()
  })

  it('closes panel on Escape key', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    expect(screen.getByTestId('group-selector-panel')).toBeDefined()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('group-selector-panel')).toBeNull()
  })

  it('closes panel when a group is selected', () => {
    renderWithRouter('/graph/fixture-a')
    fireEvent.click(screen.getByTestId('group-selector-trigger'))
    const fixtureB = screen.getAllByRole('option').find(
      (o) => o.textContent?.includes('Fixture B'),
    )!
    fireEvent.click(fixtureB)
    expect(screen.queryByTestId('group-selector-panel')).toBeNull()
  })
})
