import { render, screen, fireEvent } from '@testing-library/react'
import IterationTimeline, { type TimelineIteration } from './IterationTimeline'

function it10(over: Partial<TimelineIteration> & { iteration: number }): TimelineIteration {
  return {
    iteration: over.iteration,
    scored: over.scored ?? true,
    score: 'score' in over ? (over.score as number | null) : 0.5,
    band: over.band ?? 'fair',
  }
}

// Deliberately unsorted input to prove the component orders by iteration number.
const UNSORTED: TimelineIteration[] = [
  it10({ iteration: 3, score: 0.4, band: 'poor' }),
  it10({ iteration: 1, score: 0.95, band: 'excellent' }),
  it10({ iteration: 2, score: 0.5, band: 'fair' }),
]

describe('IterationTimeline', () => {
  it('renders the empty-state placeholder and no timeline when there are no iterations', () => {
    render(<IterationTimeline iterations={[]} />)
    expect(screen.getByTestId('timeline-empty')).toHaveTextContent(
      'No iterations recorded for this run yet.',
    )
    expect(screen.queryByTestId('iteration-timeline')).not.toBeInTheDocument()
  })

  it('renders one tick per iteration with its band, sorted ascending regardless of input order', () => {
    render(<IterationTimeline iterations={UNSORTED} />)
    expect(screen.getByRole('region', { name: 'Iteration timeline' })).toBeInTheDocument()
    const ticks = screen.getAllByTestId(/^timeline-tick-/)
    expect(ticks.map((t) => t.getAttribute('data-testid'))).toEqual([
      'timeline-tick-1',
      'timeline-tick-2',
      'timeline-tick-3',
    ])
    expect(ticks.map((t) => t.getAttribute('data-band'))).toEqual(['excellent', 'fair', 'poor'])
  })

  it('labels each tick with rounded percent and band, and band-only when unscored', () => {
    render(
      <IterationTimeline
        iterations={[it10({ iteration: 2, score: 0.5, band: 'fair' }), it10({ iteration: 4, scored: false, score: null, band: 'unscored' })]}
      />,
    )
    expect(screen.getByTestId('timeline-tick-2')).toHaveAttribute('aria-label', 'Iteration 2: 50% (fair)')
    expect(screen.getByTestId('timeline-tick-4')).toHaveAttribute('aria-label', 'Iteration 4: unscored')
  })

  it('calls onSelect with the clicked iteration number', () => {
    const onSelect = vi.fn()
    render(<IterationTimeline iterations={UNSORTED} onSelect={onSelect} />)
    fireEvent.click(screen.getByTestId('timeline-tick-2'))
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(2)
  })

  it('marks only the selected tick with aria-current', () => {
    render(<IterationTimeline iterations={UNSORTED} selected={2} />)
    expect(screen.getByTestId('timeline-tick-2')).toHaveAttribute('aria-current', 'true')
    expect(screen.getByTestId('timeline-tick-1')).not.toHaveAttribute('aria-current')
    expect(screen.getByTestId('timeline-tick-3')).not.toHaveAttribute('aria-current')
  })
})
