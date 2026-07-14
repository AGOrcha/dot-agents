import { render, screen } from '@testing-library/react'
import ScoreBadge, { type ScoreBand } from './ScoreBadge'

describe('ScoreBadge', () => {
  const bands: ScoreBand[] = ['excellent', 'good', 'fair', 'poor', 'unscored']

  it.each(bands)('renders band "%s" without crashing', (band) => {
    render(<ScoreBadge band={band} />)
    expect(screen.getByTestId('score-badge')).toBeInTheDocument()
    expect(screen.getByTestId('score-badge')).toHaveAttribute('data-band', band)
  })

  it('shows "unscored" label for unscored band', () => {
    render(<ScoreBadge band="unscored" />)
    expect(screen.getByTestId('score-badge')).toHaveTextContent('unscored')
  })

  it('shows band name for scored band without score value', () => {
    render(<ScoreBadge band="excellent" />)
    expect(screen.getByTestId('score-badge')).toHaveTextContent('excellent')
  })

  it('shows band and percentage when score is provided', () => {
    render(<ScoreBadge band="good" score={0.82} />)
    expect(screen.getByTestId('score-badge')).toHaveTextContent('good (82%)')
  })

  it.each([
    ['excellent', 'green'],
    ['good', 'blue'],
    ['fair', 'yellow'],
    ['poor', 'red'],
    ['unscored', 'gray'],
  ] as [ScoreBand, string][])('styles the "%s" band with %s-family classes', (band, color) => {
    render(<ScoreBadge band={band} />)
    expect(screen.getByTestId('score-badge').className).toContain(color)
  })
})
