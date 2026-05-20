import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { CrossStackBadge } from '@/components/flows/CrossStackBadge'

vi.mock('lucide-react', () => ({
  GitMerge: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} data-testid="git-merge-icon" />,
}))

describe('CrossStackBadge', () => {
  it('renders with default repo count', () => {
    render(<CrossStackBadge />)
    expect(screen.getByText('2 repos')).toBeInTheDocument()
  })

  it('renders with link method label', () => {
    render(<CrossStackBadge linkMethod="http" />)
    expect(screen.getByText('via HTTP')).toBeInTheDocument()
  })

  it('renders kafka link method', () => {
    render(<CrossStackBadge linkMethod="kafka_topic" />)
    expect(screen.getByText('via Kafka')).toBeInTheDocument()
  })

  it('renders custom repo count', () => {
    render(<CrossStackBadge repoCount={3} />)
    expect(screen.getByText('3 repos')).toBeInTheDocument()
  })

  it('has aria-label for accessibility', () => {
    render(<CrossStackBadge repoCount={2} />)
    // The span has aria-label starting with "Cross-stack"
    const badge = screen.getByLabelText(/cross-stack/i)
    expect(badge).toBeInTheDocument()
  })
})
