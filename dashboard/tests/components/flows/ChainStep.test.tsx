import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ChainStep } from '@/components/flows/ChainStep'
import type { ProcessStep } from '@/types/api'

vi.mock('lucide-react', () => ({
  FunctionSquare: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Database: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  File: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
  Globe: (props: React.SVGProps<SVGSVGElement>) => <svg {...props} />,
}))

vi.mock('@/lib/colors', () => ({
  repoColor: () => ({ bg: 'bg-sky-900/40', text: 'text-sky-300', dot: 'bg-sky-400' }),
}))

const step: ProcessStep = {
  entity_id: 'acme-api::UserService.list',
  label: 'UserService.list',
  source_file: 'acme_api/services/user_service.py',
  start_line: 34,
  repo: 'acme-api',
  step_index: 1,
  edge_kind: 'CALLS',
}

describe('ChainStep', () => {
  it('renders step label', () => {
    render(<ChainStep step={step} />)
    expect(screen.getByText('UserService.list')).toBeInTheDocument()
  })

  it('renders edge label for CALLS', () => {
    render(<ChainStep step={step} />)
    expect(screen.getByText('calls')).toBeInTheDocument()
  })

  it('does not render edge label for ENTRY_POINT_OF', () => {
    render(<ChainStep step={{ ...step, step_index: 0, edge_kind: 'ENTRY_POINT_OF' }} />)
    expect(screen.queryByText(/calls|step|entry/i)).toBeNull()
  })

  it('applies phantom styles when isPhantom=true', () => {
    render(<ChainStep step={step} isPhantom />)
    const btn = screen.getByRole('button')
    expect(btn.className).toContain('border-dashed')
  })

  it('applies focused styles when isFocused=true', () => {
    render(<ChainStep step={step} isFocused />)
    const btn = screen.getByRole('button')
    expect(btn.className).toContain('ring-1')
  })

  it('calls onClick when clicked', () => {
    const onClick = vi.fn()
    render(<ChainStep step={step} onClick={onClick} />)
    fireEvent.click(screen.getByRole('button'))
    expect(onClick).toHaveBeenCalledWith(step)
  })

  it('calls onClick on Enter key', () => {
    const onClick = vi.fn()
    render(<ChainStep step={step} onClick={onClick} />)
    fireEvent.keyDown(screen.getByRole('button'), { key: 'Enter' })
    expect(onClick).toHaveBeenCalledWith(step)
  })
})
