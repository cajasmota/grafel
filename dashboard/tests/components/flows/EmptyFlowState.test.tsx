import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { EmptyFlowState } from '@/components/flows/EmptyFlowState'

vi.mock('lucide-react', () => ({
  Workflow: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} data-testid="workflow-icon" />,
  SearchX: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} data-testid="searchx-icon" />,
}))

describe('EmptyFlowState', () => {
  it('renders entry-point prompt when group is present', () => {
    render(<EmptyFlowState hasGroup />)
    expect(screen.getByText('Select an entry point')).toBeInTheDocument()
  })

  it('renders no-group message when hasGroup=false', () => {
    render(<EmptyFlowState hasGroup={false} />)
    expect(screen.getByText('No group selected')).toBeInTheDocument()
  })

  it('defaults to hasGroup=true', () => {
    render(<EmptyFlowState />)
    expect(screen.getByText('Select an entry point')).toBeInTheDocument()
  })

  it('has role=status for a11y', () => {
    render(<EmptyFlowState />)
    expect(screen.getByRole('status')).toBeInTheDocument()
  })
})
