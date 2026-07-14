/**
 * Render smoke tests for the four skeleton view components.
 *
 * Each view wraps a useQuery call and renders three branches:
 *   loading  — while the query is in flight
 *   error    — when the API is unreachable / returns non-2xx
 *   data     — when the API returns a successful payload
 *
 * These tests exercise all three branches per view so that per-file coverage
 * clears the SonarCloud new_coverage gate. Full feature tests (sortable grid,
 * chart interactions, signal breakdown table) are deferred to t09/t10/t15.
 * // t16: real view tests when implemented
 */
import React from 'react'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import AggregateView from './AggregateView'
import RunDetailView from './RunDetailView'
import IterationDetailView from './IterationDetailView'
import RubricView from './RubricView'

// Recharts' ResponsiveContainer needs ResizeObserver, which jsdom lacks (real
// browsers provide it). Stub it so the populated aggregate view exercises the
// successful dashboard path instead of tripping its error boundary.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver

// ---- helpers ---------------------------------------------------------------

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: 0 } } })
}

function renderView(
  ui: React.ReactElement,
  { initialPath = '/', path = '/' }: { initialPath?: string; path?: string } = {},
) {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path={path} element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockFetch(status: number, body: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify(body), { status }),
  )
}

function errorBody() {
  return { error: { code: 'internal', message: 'not reachable' } }
}

afterEach(() => {
  vi.restoreAllMocks()
})

// ---- AggregateView ---------------------------------------------------------

describe('AggregateView', () => {
  it('renders the Runs heading', async () => {
    mockFetch(500, errorBody())
    renderView(<AggregateView />, { initialPath: '/', path: '/' })
    expect(await screen.findByRole('heading', { name: /runs/i })).toBeInTheDocument()
  })

  it('shows error state when API is unreachable', async () => {
    mockFetch(500, errorBody())
    renderView(<AggregateView />, { initialPath: '/', path: '/' })
    expect(await screen.findByText(/not reachable/i)).toBeInTheDocument()
  })

  it('shows empty-state message when API returns an empty list', async () => {
    mockFetch(200, { data: [] })
    renderView(<AggregateView />, { initialPath: '/', path: '/' })
    expect(await screen.findByText(/no runs found/i)).toBeInTheDocument()
  })

  it('shows run count when API returns a populated list', async () => {
    mockFetch(200, {
      data: [
        {
          session_id: 's-1',
          rubric_version: '2.1.0',
          iteration_count: 3,
          scored: true,
          score: 0.9,
          band: 'excellent',
        },
      ],
    })
    renderView(<AggregateView />, { initialPath: '/', path: '/' })
    expect(await screen.findByText(/1 run\(s\) loaded/i)).toBeInTheDocument()
  })
})

// ---- RunDetailView ---------------------------------------------------------

describe('RunDetailView', () => {
  it('renders the Run Detail heading', async () => {
    mockFetch(500, errorBody())
    renderView(<RunDetailView />, { initialPath: '/runs/sess-abc', path: '/runs/:sessionId' })
    expect(await screen.findByRole('heading', { name: /run detail/i })).toBeInTheDocument()
  })

  it('displays the session id from the URL', () => {
    mockFetch(500, errorBody())
    renderView(<RunDetailView />, { initialPath: '/runs/sess-abc', path: '/runs/:sessionId' })
    expect(screen.getByText('sess-abc')).toBeInTheDocument()
  })

  it('shows error state when API is unreachable', async () => {
    mockFetch(500, errorBody())
    renderView(<RunDetailView />, { initialPath: '/runs/sess-abc', path: '/runs/:sessionId' })
    expect(await screen.findByText(/not found or api unavailable/i)).toBeInTheDocument()
  })

  it('shows harness and iteration count when API returns run data', async () => {
    mockFetch(200, {
      data: {
        session_id: 'sess-abc',
        harness: 'claude-code',
        iteration_count: 5,
        rubric_version: '2.1.0',
        scored: true,
        score: 0.85,
        band: 'excellent',
      },
    })
    renderView(<RunDetailView />, { initialPath: '/runs/sess-abc', path: '/runs/:sessionId' })
    expect(await screen.findByText('claude-code')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('shows em-dash for harness when API returns run without a harness field', async () => {
    mockFetch(200, {
      data: {
        session_id: 'sess-noharness',
        // harness deliberately omitted — component renders '—'
        iteration_count: 2,
        rubric_version: '2.1.0',
        scored: false,
        score: null,
        band: 'unscored',
      },
    })
    renderView(<RunDetailView />, { initialPath: '/runs/sess-noharness', path: '/runs/:sessionId' })
    expect(await screen.findByText('2')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows em-dash for session id when path param is absent', () => {
    mockFetch(500, errorBody())
    // Render without :sessionId param so sessionId defaults to ''
    renderView(<RunDetailView />, { initialPath: '/', path: '/' })
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

// ---- IterationDetailView ---------------------------------------------------

describe('IterationDetailView', () => {
  it('renders the Iteration Detail heading', async () => {
    mockFetch(500, errorBody())
    renderView(<IterationDetailView />, { initialPath: '/iterations/5', path: '/iterations/:n' })
    expect(await screen.findByRole('heading', { name: /iteration detail/i })).toBeInTheDocument()
  })

  it('displays the iteration number from the URL', () => {
    mockFetch(500, errorBody())
    renderView(<IterationDetailView />, { initialPath: '/iterations/7', path: '/iterations/:n' })
    expect(screen.getByText('7')).toBeInTheDocument()
  })

  it('shows error state when API is unreachable', async () => {
    mockFetch(500, errorBody())
    renderView(<IterationDetailView />, {
      initialPath: '/iterations/5',
      path: '/iterations/:n',
    })
    expect(await screen.findByText(/not found or api unavailable/i)).toBeInTheDocument()
  })

  it('does not fetch when n is not a finite integer', () => {
    const spy = mockFetch(500, errorBody())
    renderView(<IterationDetailView />, { initialPath: '/iterations/nan', path: '/iterations/:n' })
    // enabled:false when parseInt returns NaN — no API call should be made
    expect(spy).not.toHaveBeenCalled()
  })

  it('shows wave and task_id when API returns iteration data', async () => {
    mockFetch(200, {
      data: {
        iteration: 5,
        wave: 'r2',
        task_id: 't08',
        rubric_version: '2.1.0',
        scored: true,
        score: 0.8,
        band: 'good',
      },
    })
    renderView(<IterationDetailView />, { initialPath: '/iterations/5', path: '/iterations/:n' })
    expect(await screen.findByText('r2')).toBeInTheDocument()
    expect(screen.getByText('t08')).toBeInTheDocument()
  })

  it('shows em-dash fallbacks when data fields are absent and scored is false', async () => {
    mockFetch(200, {
      data: {
        iteration: 3,
        rubric_version: '2.1.0',
        scored: false,
        score: null,
        band: 'unscored',
        // wave, task_id deliberately omitted — component renders '—'
      },
    })
    renderView(<IterationDetailView />, { initialPath: '/iterations/3', path: '/iterations/:n' })
    // scored: false renders 'no'
    expect(await screen.findByText('no')).toBeInTheDocument()
    // wave and task_id fallback to '—' (there are two of them; just confirm at least one)
    const dashes = await screen.findAllByText('—')
    expect(dashes.length).toBeGreaterThanOrEqual(2)
  })

  it('includes iter_log_dir as a query param when fetching iteration detail', async () => {
    const spy = mockFetch(200, {
      data: {
        iteration: 7,
        rubric_version: '2.1.0',
        scored: false,
        score: null,
        band: 'unscored',
      },
    })
    renderView(<IterationDetailView />, {
      initialPath: '/iterations/7?iter_log_dir=%2Fvar%2Flogs',
      path: '/iterations/:n',
    })
    // Wait for the fetch to happen
    await screen.findByRole('heading', { name: /iteration detail/i })
    // The queryFn builds the qs string when iterLogDir is truthy
    expect(spy).toHaveBeenCalledWith(
      expect.stringContaining('iter_log_dir='),
      expect.anything(),
    )
  })

  it('shows em-dash for iteration number when path param is absent', () => {
    mockFetch(500, errorBody())
    // Render without the :n param so n defaults to ''
    renderView(<IterationDetailView />, { initialPath: '/', path: '/' })
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

// ---- RubricView ------------------------------------------------------------

describe('RubricView', () => {
  it('renders the Rubric heading', async () => {
    mockFetch(500, errorBody())
    renderView(<RubricView />, { initialPath: '/rubric', path: '/rubric' })
    expect(await screen.findByRole('heading', { name: /rubric/i })).toBeInTheDocument()
  })

  it('shows error state when API is unreachable', async () => {
    mockFetch(500, errorBody())
    renderView(<RubricView />, { initialPath: '/rubric', path: '/rubric' })
    expect(await screen.findByText(/rubric unavailable/i)).toBeInTheDocument()
  })

  it('renders rubric version and signal table when API returns rubric data', async () => {
    mockFetch(200, {
      data: {
        version: '2.1.0',
        combination: 'weighted_mean_renormalized',
        signals: [
          {
            id: 'landed',
            label: 'Landed',
            weight: 0.25,
            description: 'Code landed in main',
            two_way: true,
          },
        ],
        bands: [{ name: 'excellent', min: 0.85 }],
      },
    })
    renderView(<RubricView />, { initialPath: '/rubric', path: '/rubric' })
    expect(await screen.findByText('2.1.0')).toBeInTheDocument()
    // RubricView renders sig.id (not sig.label) in the Signal column
    expect(screen.getByText('landed')).toBeInTheDocument()
    // weight cell: {(0.25 * 100).toFixed(0)}% → "25" + "%" text nodes
    expect(screen.getByText(/25\s*%/)).toBeInTheDocument()
    expect(screen.getByText('Code landed in main')).toBeInTheDocument()
  })
})
