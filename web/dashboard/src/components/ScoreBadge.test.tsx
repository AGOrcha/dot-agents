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

  it('uses green styling for excellent band', () => {
    render(<ScoreBadge band="excellent" />)
    const badge = screen.getByTestId('score-badge')
    expect(badge.className).toContain('green')
  })

  it('uses blue styling for good band', () => {
    render(<ScoreBadge band="good" />)
    const badge = screen.getByTestId('score-badge')
    expect(badge.className).toContain('blue')
  })

  it('uses yellow styling for fair band', () => {
    render(<ScoreBadge band="fair" />)
    const badge = screen.getByTestId('score-badge')
    expect(badge.className).toContain('yellow')
  })

  it('uses red styling for poor band', () => {
    render(<ScoreBadge band="poor" />)
    const badge = screen.getByTestId('score-badge')
    expect(badge.className).toContain('red')
  })

  it('uses gray styling for unscored band', () => {
    render(<ScoreBadge band="unscored" />)
    const badge = screen.getByTestId('score-badge')
    expect(badge.className).toContain('gray')
  })
})
