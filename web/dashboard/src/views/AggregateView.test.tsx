import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import AggregateView from './AggregateView'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

// Recharts' ResponsiveContainer needs ResizeObserver, which jsdom lacks (real
// browsers provide it). Stub it so the trend charts mount under the renderer.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: 0 } } })
}

function renderView() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter>
        <AggregateView />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockFetch(status: number, body: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(body), { status }))
}

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

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AggregateView', () => {
  it('renders the Runs heading', async () => {
    mockFetch(200, { data: [] })
    renderView()
    expect(await screen.findByRole('heading', { name: /runs/i })).toBeInTheDocument()
  })

  it('shows a loading skeleton while the runs query is pending', () => {
    const { promise } = Promise.withResolvers<Response>()
    vi.spyOn(globalThis, 'fetch').mockReturnValue(promise)
    renderView()
    expect(screen.getByTestId('runs-skeleton')).toBeInTheDocument()
  })

  it('shows the error state when the API is unreachable', async () => {
    mockFetch(500, { error: { code: 'internal', message: 'not reachable' } })
    renderView()
    const err = await screen.findByTestId('aggregate-error')
    expect(err).toHaveTextContent(/not reachable/i)
  })

  it('shows the empty-state when the API returns no runs', async () => {
    mockFetch(200, { data: [] })
    renderView()
    expect(await screen.findByTestId('aggregate-empty')).toBeInTheDocument()
  })

  it('renders the grid and both trend charts for a populated list', async () => {
    mockFetch(200, {
      data: [
        run({
          session_id: 'alpha',
          harness: 'claude-code',
          last_iteration: 3,
          iteration_count: 3,
          score: 0.9,
          band: 'excellent',
          mean_cache_hit_rate: 0.8,
          last_update: '2026-03-01T10:00:00Z',
        }),
      ],
    })
    renderView()
    expect(await screen.findByTestId('runs-grid')).toBeInTheDocument()
    expect(screen.getByTestId('score-trend-chart')).toBeInTheDocument()
    expect(screen.getByTestId('cache-trend-chart')).toBeInTheDocument()
    expect(screen.getByTestId('run-row-alpha')).toBeInTheDocument()
    expect(screen.getByTestId('runs-count')).toHaveTextContent('1 run(s) loaded.')
  })
})
