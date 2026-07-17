import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import RubricView from './RubricView'
import type { R2DashboardRubricDTO } from '../api/types.gen'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: 0 } } })
}

function renderView() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter>
        <RubricView />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockFetch(status: number, body: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(body), { status }))
}

const RUBRIC: R2DashboardRubricDTO = {
  version: '2.1.0',
  combination: 'weighted_mean_renormalized',
  signals: [
    { id: 'landed', label: 'Landed', weight: 0.5, description: 'Change was committed.', two_way: true },
    { id: 'tests', label: 'Tests', weight: 0.3, description: 'Tests ran and passed.', two_way: true },
    { id: 'scope', label: 'Scope', weight: 0.2, description: 'Stayed within scope.', two_way: true },
  ],
  bands: [
    { name: 'excellent', min: 0.8 },
    { name: 'good', min: 0.6 },
    { name: 'poor', min: 0 },
  ],
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RubricView', () => {
  it('renders the Rubric heading', async () => {
    mockFetch(200, { data: RUBRIC })
    renderView()
    expect(await screen.findByRole('heading', { name: /rubric/i })).toBeInTheDocument()
  })

  it('issues a GET to the /rubric endpoint', async () => {
    const spy = mockFetch(200, { data: RUBRIC })
    renderView()
    await screen.findByTestId('rubric-signals-table')
    expect(spy).toHaveBeenCalledTimes(1)
    const url = spy.mock.calls[0][0]
    expect(String(url)).toMatch(/\/rubric$/)
  })

  it('shows the loading state while the rubric query is pending', () => {
    const { promise } = Promise.withResolvers<Response>()
    vi.spyOn(globalThis, 'fetch').mockReturnValue(promise)
    renderView()
    expect(screen.getByTestId('rubric-loading')).toBeInTheDocument()
  })

  it('shows the error state when the API is unreachable', async () => {
    mockFetch(500, { error: { code: 'internal', message: 'rubric backend down' } })
    renderView()
    const err = await screen.findByTestId('rubric-error')
    expect(err).toHaveTextContent(/rubric backend down/i)
  })

  it('renders the version, signal weights, and score bands for a loaded rubric', async () => {
    mockFetch(200, { data: RUBRIC })
    renderView()

    // Version + combination header.
    expect(await screen.findByText('2.1.0')).toBeInTheDocument()
    expect(screen.getByText('weighted_mean_renormalized')).toBeInTheDocument()

    // Weights render (via RubricTable).
    expect(within(screen.getByTestId('rubric-signal-row-landed')).getByText('50%')).toBeInTheDocument()
    expect(within(screen.getByTestId('rubric-signal-row-tests')).getByText('30%')).toBeInTheDocument()
    expect(within(screen.getByTestId('rubric-signal-row-scope')).getByText('20%')).toBeInTheDocument()

    // Bands render as ranges.
    expect(within(screen.getByTestId('rubric-band-row-excellent')).getByText('0.80 – 1.00')).toBeInTheDocument()
    expect(within(screen.getByTestId('rubric-band-row-poor')).getByText('0.00 – 0.60')).toBeInTheDocument()
  })
})
