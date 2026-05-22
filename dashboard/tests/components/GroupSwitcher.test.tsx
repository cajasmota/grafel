import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { GroupSwitcher } from '@/components/layout/GroupSwitcher'
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
        <Route path="/:surface/:group" element={
          <GroupSwitcher groups={mockGroups} />
        } />
      </Routes>
    </MemoryRouter>,
  )
}

describe('GroupSwitcher', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders all groups', () => {
    renderWithRouter('/graph/fixture-a')
    expect(screen.getByText('Fixture A')).toBeDefined()
    expect(screen.getByText('Fixture B')).toBeDefined()
  })

  it('marks the active group as selected', () => {
    renderWithRouter('/graph/fixture-a')
    const activeOption = screen.getAllByRole('option').find(
      (o) => o.getAttribute('aria-selected') === 'true',
    )
    expect(activeOption?.textContent).toContain('Fixture A')
  })

  it('filters groups by query', () => {
    renderWithRouter('/graph/fixture-a')
    const input = screen.getByRole('searchbox', { name: 'Filter groups' })
    fireEvent.change(input, { target: { value: 'B' } })
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0].textContent).toContain('Fixture B')
  })

  it('renders health status dots for all groups', () => {
    renderWithRouter('/graph/fixture-a')
    // Each group option should have a health status dot with aria-label starting with "Health:"
    const dots = screen.getAllByRole('option').flatMap(
      (o) => Array.from(o.querySelectorAll('[aria-label^="Health:"]')),
    )
    expect(dots.length).toBeGreaterThanOrEqual(2)
  })

  it('active group has checkmark and health dot independently', () => {
    renderWithRouter('/graph/fixture-a')
    const activeOption = screen.getAllByRole('option').find(
      (o) => o.getAttribute('aria-selected') === 'true',
    )!
    // Checkmark icon should be present (not invisible)
    const checkSpan = activeOption.querySelector('[aria-hidden]')
    expect(checkSpan).not.toBeNull()
    // Health dot should carry "Health:" tooltip
    const healthDot = activeOption.querySelector('[aria-label^="Health:"]')
    expect(healthDot).not.toBeNull()
  })

  it('calls onNavigate when a group is selected', () => {
    const onNavigate = vi.fn()
    render(
      <MemoryRouter initialEntries={['/graph/fixture-a']}>
        <Routes>
          <Route path="/:surface/:group" element={
            <GroupSwitcher groups={mockGroups} onNavigate={onNavigate} />
          } />
        </Routes>
      </MemoryRouter>,
    )
    // Click the outer button row for Fixture B (not the pin toggle)
    const fixtureB = screen.getAllByRole('option').find(
      (o) => o.textContent?.includes('Fixture B'),
    )!
    // Find the group name text span to click (avoids the pin toggle span)
    const nameSpan = fixtureB.querySelector('[class*="font-mono"]') as HTMLElement
    fireEvent.click(nameSpan ?? fixtureB)
    expect(onNavigate).toHaveBeenCalledOnce()
  })

  it('shows "No groups match" when filter has no results', () => {
    renderWithRouter('/graph/fixture-a')
    const input = screen.getByRole('searchbox', { name: 'Filter groups' })
    fireEvent.change(input, { target: { value: 'zzz' } })
    expect(screen.getByText('No groups match')).toBeDefined()
  })
})
