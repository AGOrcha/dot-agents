import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useParams } from 'react-router-dom'
import RunDetailView from './RunDetailView'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: 0 } } })
}

function mockFetch(status: number, body: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(
    async () => new Response(JSON.stringify(body), { status }),
  )
}

function errorBody() {
  return { error: { code: 'internal', message: 'not reachable' } }
}

// Probe route so a navigation into /iterations/:n is observable via the param.
function IterationProbe() {
  const { n } = useParams<{ n: string }>()
  return <div data-testid="iter-probe">{n}</div>
}

function renderRun(initialPath: string) {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/runs/:sessionId" element={<RunDetailView />} />
          <Route path="/iterations/:n" element={<IterationProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

const RUN: R2DashboardRunSessionDTO = {
  session_id: 'sess-abc',
  harness: 'claude-code',
  rubric_version: '2.1.0',
  iteration_count: 3,
  scored: true,
  score: 0.85,
  band: 'excellent',
  per_iteration: [
    { iteration: 1, scored: true, score: 0.95, band: 'excellent' },
    { iteration: 2, scored: true, score: 0.5, band: 'fair' },
    { iteration: 3, scored: false, score: null, band: 'unscored' },
  ],
}

describe('RunDetailView', () => {
  it('renders the header with session id, harness and the mean-score badge', async () => {
    mockFetch(200, { data: RUN })
    renderRun('/runs/sess-abc')

    expect(await screen.findByText('claude-code')).toBeInTheDocument()
    expect(screen.getByText('sess-abc')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /run detail/i })).toBeInTheDocument()
    expect(screen.getByTestId('score-badge')).toHaveAttribute('data-band', 'excellent')
  })

  it('renders the error state when the API is unreachable', async () => {
    mockFetch(500, errorBody())
    renderRun('/runs/sess-abc')
    expect(await screen.findByText(/not found or api unavailable/i)).toBeInTheDocument()
    expect(screen.queryByTestId('iteration-timeline')).not.toBeInTheDocument()
  })

  it('renders a timeline tick per per_iteration entry', async () => {
    mockFetch(200, { data: RUN })
    renderRun('/runs/sess-abc')
    await screen.findByTestId('iteration-timeline')
    expect(screen.getAllByTestId(/^timeline-tick-/)).toHaveLength(3)
    expect(screen.getByTestId('timeline-tick-2')).toHaveAttribute('data-band', 'fair')
  })

  it('navigates to /iterations/:n when a timeline tick is clicked', async () => {
    mockFetch(200, { data: RUN })
    renderRun('/runs/sess-abc')
    await screen.findByTestId('iteration-timeline')
    fireEvent.click(screen.getByTestId('timeline-tick-2'))
    expect(await screen.findByTestId('iter-probe')).toHaveTextContent('2')
  })
})
