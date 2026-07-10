import { render, screen, within } from '@testing-library/react'
import CacheTrendChart, { buildCacheSeries } from './CacheTrendChart'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

// Recharts' ResponsiveContainer needs ResizeObserver, which jsdom lacks (real
// browsers provide it). Stub it so the chart mounts under the test renderer.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver

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

describe('buildCacheSeries', () => {
  it('excludes runs without cache telemetry (mean_cache_hit_rate == null)', () => {
    const series = buildCacheSeries([
      run({ session_id: 'a', mean_cache_hit_rate: 0.6, last_update: '2026-01-01T00:00:00Z' }),
      run({ session_id: 'b', mean_cache_hit_rate: null, last_update: '2026-01-02T00:00:00Z' }),
      run({ session_id: 'c', last_update: '2026-01-03T00:00:00Z' }),
    ])
    expect(series.map((p) => p.key)).toEqual(['a'])
  })

  it('orders oldest→newest and windows to the most recent N', () => {
    const runs = [1, 2, 3].map((n) =>
      run({ session_id: `s${n}`, mean_cache_hit_rate: n / 10, last_update: `2026-0${n}-01T00:00:00Z` }),
    )
    expect(buildCacheSeries(runs, 2).map((p) => p.key)).toEqual(['s2', 's3'])
  })

  it('maps the rate onto each point', () => {
    const series = buildCacheSeries([
      run({ session_id: 'a', mean_cache_hit_rate: 0.83, last_iteration: 4, last_update: '2026-01-01T00:00:00Z' }),
    ])
    expect(series[0]).toMatchObject({ key: 'a', label: '#4', rate: 0.83 })
  })
})

describe('CacheTrendChart', () => {
  it('shows an empty-state when no run captured cache telemetry', () => {
    render(<CacheTrendChart runs={[run({ session_id: 'x', mean_cache_hit_rate: null })]} />)
    expect(screen.getByTestId('cache-trend-empty')).toBeInTheDocument()
  })

  it('renders an accessible data point per run as a percentage', () => {
    render(
      <CacheTrendChart
        runs={[
          run({ session_id: 'a', mean_cache_hit_rate: 0.75, last_iteration: 2, last_update: '2026-01-01T00:00:00Z' }),
          run({ session_id: 'b', mean_cache_hit_rate: 0.5, last_iteration: 9, last_update: '2026-01-02T00:00:00Z' }),
        ]}
      />,
    )
    const list = within(screen.getByTestId('cache-trend-data')).getAllByRole('listitem')
    expect(list.map((li) => li.textContent)).toEqual(['#2: 75%', '#9: 50%'])
  })
})
