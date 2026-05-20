import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { FlowRow } from '@/components/flows/FlowRow'
import type { Process } from '@/types/api'

vi.mock('lucide-react', () => ({
  Workflow: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Radio: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Clock: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Cpu: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  GitMerge: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
}))

vi.mock('@/lib/colors', () => ({
  repoColor: () => ({ bg: 'bg-sky-900/40', text: 'text-sky-300', dot: 'bg-sky-400' }),
}))

const baseProcess: Process = {
  process_id: 'proc-001',
  repo: 'acme-api',
  label: 'GET /api/v1/users',
  entry_id: 'acme-api::UserListView',
  entry_name: 'UserListView',
  terminal_id: 'acme-api::user_table',
  step_count: 4,
  cross_stack: false,
  entity_kind: 'http',
  chain_labels: ['UserListView', 'UserService.list', 'user_table'],
}

describe('FlowRow', () => {
  it('renders entry name', () => {
    render(<FlowRow process={baseProcess} />)
    expect(screen.getByText('UserListView')).toBeInTheDocument()
  })

  it('renders step count badge', () => {
    render(<FlowRow process={baseProcess} />)
    expect(screen.getByTitle('4 steps')).toBeInTheDocument()
  })

  it('does not render cross-stack badge when cross_stack=false', () => {
    render(<FlowRow process={baseProcess} />)
    expect(screen.queryByText(/repos/i)).toBeNull()
  })

  it('renders cross-stack badge when cross_stack=true', () => {
    render(<FlowRow process={{ ...baseProcess, cross_stack: true }} />)
    // CrossStackBadge renders "2 repos" by default
    expect(screen.getByText('2 repos')).toBeInTheDocument()
  })

  it('calls onSelect when clicked', () => {
    const onSelect = vi.fn()
    render(<FlowRow process={baseProcess} onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('row'))
    expect(onSelect).toHaveBeenCalledWith('proc-001')
  })

  it('calls onSelect on Enter key', () => {
    const onSelect = vi.fn()
    render(<FlowRow process={baseProcess} onSelect={onSelect} />)
    fireEvent.keyDown(screen.getByRole('row'), { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('proc-001')
  })

  it('applies selected style when isSelected=true', () => {
    render(<FlowRow process={baseProcess} isSelected />)
    const row = screen.getByRole('row')
    expect(row).toHaveAttribute('aria-selected', 'true')
  })
})
