import { render, screen, within } from '@testing-library/react'
import ScoreTrendChart, { buildScoreSeries } from './ScoreTrendChart'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

function run(over: Partial<R2DashboardRunSessionDTO> & { session_id: string }): R2DashboardRunSessionDTO {
  return {
    session_id: over.session_id,
    rubric_version: '2.1.0',
    iteration_count: 1,
    scored: true,
    score: 0.5,
    band: 'fair',
    ...over,
  }
}

describe('buildScoreSeries', () => {
  it('excludes unscored runs (score === null)', () => {
    const series = buildScoreSeries([
      run({ session_id: 'a', scored: true, score: 0.8, band: 'good', last_update: '2026-01-01T00:00:00Z' }),
      run({ session_id: 'b', scored: false, score: null, band: 'unscored', last_update: '2026-01-02T00:00:00Z' }),
    ])
    expect(series.map((p) => p.key)).toEqual(['a'])
  })

  it('orders oldest→newest by last_update', () => {
    const series = buildScoreSeries([
      run({ session_id: 'newer', score: 0.9, last_update: '2026-03-01T00:00:00Z' }),
      run({ session_id: 'older', score: 0.4, last_update: '2026-01-01T00:00:00Z' }),
      run({ session_id: 'mid', score: 0.6, last_update: '2026-02-01T00:00:00Z' }),
    ])
    expect(series.map((p) => p.key)).toEqual(['older', 'mid', 'newer'])
  })

  it('keeps only the most recent windowSize runs', () => {
    const runs = [1, 2, 3, 4, 5].map((n) =>
      run({ session_id: `s${n}`, score: n / 10, last_update: `2026-0${n}-01T00:00:00Z` }),
    )
    const series = buildScoreSeries(runs, 2)
    expect(series.map((p) => p.key)).toEqual(['s4', 's5'])
  })

  it('labels a point by its last iteration, falling back to session id', () => {
    const series = buildScoreSeries([
      run({ session_id: 'has-iter', score: 0.7, last_iteration: 12, last_update: '2026-01-01T00:00:00Z' }),
      run({ session_id: 'no-iter', score: 0.7, last_update: '2026-01-02T00:00:00Z' }),
    ])
    expect(series.map((p) => p.label)).toEqual(['#12', 'no-iter'])
  })
})

describe('ScoreTrendChart', () => {
  it('shows an empty-state when no runs are scored', () => {
    render(<ScoreTrendChart runs={[run({ session_id: 'x', scored: false, score: null, band: 'unscored' })]} />)
    expect(screen.getByTestId('score-trend-empty')).toBeInTheDocument()
  })

  it('renders an accessible data point per scored run as a percentage', () => {
    render(
      <ScoreTrendChart
        runs={[
          run({ session_id: 'a', score: 0.9, last_iteration: 3, last_update: '2026-01-01T00:00:00Z' }),
          run({ session_id: 'b', score: 0.42, last_iteration: 7, last_update: '2026-01-02T00:00:00Z' }),
        ]}
      />,
    )
    const list = within(screen.getByTestId('score-trend-data')).getAllByRole('listitem')
    expect(list.map((li) => li.textContent)).toEqual(['#3: 90%', '#7: 42%'])
  })
})
