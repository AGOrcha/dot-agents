import React from 'react'
import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import IterationDetailView from './IterationDetailView'
import type { R2DashboardIterationDTO } from '../api/types.gen'

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

function renderIteration(initialPath: string, path = '/iterations/:n') {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path={path} element={<IterationDetailView />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

const FULL: R2DashboardIterationDTO = {
  iteration: 5,
  wave: 'r2',
  task_id: 't10',
  commit: 'deadbeef',
  rubric_version: '2.1.0',
  scored: true,
  score: 0.8,
  band: 'good',
  files_changed: 7,
  lines_added: 120,
  lines_removed: 30,
  retries: 4,
  verifiers: [{ type: 'unit', status: 'pass', tests_added: 12, gate_passed: true }],
  breakdown: [
    {
      signal: 'landed',
      label: 'Landed',
      present: true,
      sub_score: 0.9,
      nominal_weight: 0.2,
      effective_weight: 0.2,
      contribution: 0.18,
    },
  ],
  integrity: [
    {
      signal: 'landed',
      role: 'impl',
      claimed: { present: true, sub_score: 0.9 },
      observed: { present: true, sub_score: 0.7 },
      comparable: true,
      delta: -0.2,
    },
  ],
}

describe('IterationDetailView', () => {
  it('renders the header with the score badge from the DTO', async () => {
    mockFetch(200, { data: FULL })
    renderIteration('/iterations/5')
    expect(await screen.findByRole('heading', { name: /iteration detail/i })).toBeInTheDocument()
    expect(await screen.findByTestId('score-badge')).toHaveAttribute('data-band', 'good')
  })

  it('renders the record fields with signed line deltas', async () => {
    mockFetch(200, { data: FULL })
    renderIteration('/iterations/5')
    await screen.findByText('t10') // task_id
    expect(screen.getByText('deadbeef')).toBeInTheDocument() // commit
    expect(screen.getByText('7')).toBeInTheDocument() // files_changed
    expect(screen.getByText('+120')).toBeInTheDocument() // lines_added
    expect(screen.getByText('-30')).toBeInTheDocument() // lines_removed
    expect(screen.getByText('4')).toBeInTheDocument() // retries
  })

  it('renders the verifiers list with status, tests added and gate outcome', async () => {
    mockFetch(200, { data: FULL })
    renderIteration('/iterations/5')
    const li = await screen.findByTestId('verifier-unit')
    expect(within(li).getByText('unit')).toBeInTheDocument()
    expect(within(li).getByText('pass')).toBeInTheDocument()
    expect(within(li).getByText('+12 tests')).toBeInTheDocument()
    expect(within(li).getByText('gate passed')).toBeInTheDocument()
  })

  it('wires the SignalBreakdown and IntegrityPanel tables from the DTO', async () => {
    mockFetch(200, { data: FULL })
    renderIteration('/iterations/5')
    expect(await screen.findByTestId('signal-breakdown')).toBeInTheDocument()
    expect(screen.getByTestId('integrity-panel')).toBeInTheDocument()
    expect(screen.getByTestId('breakdown-row-landed')).toBeInTheDocument()
    expect(screen.getByTestId('integrity-row-landed')).toBeInTheDocument()
  })

  it('renders the breakdown empty-state for an unscored iteration with no breakdown', async () => {
    mockFetch(200, {
      data: {
        iteration: 3,
        rubric_version: '2.1.0',
        scored: false,
        score: null,
        band: 'unscored',
      } satisfies R2DashboardIterationDTO,
    })
    renderIteration('/iterations/3')
    expect(await screen.findByTestId('breakdown-empty')).toBeInTheDocument()
    expect(screen.queryByTestId('signal-breakdown')).not.toBeInTheDocument()
  })

  it('does not fetch when n is not a finite integer', () => {
    const spy = mockFetch(200, { data: FULL })
    renderIteration('/iterations/nan')
    expect(spy).not.toHaveBeenCalled()
  })

  it('loads the full record standalone via a deep-link (no run context)', async () => {
    mockFetch(200, { data: FULL })
    renderIteration('/iterations/5')
    // Heading renders during loading, so await a data-dependent field before asserting.
    expect(await screen.findByText('t10')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /iteration detail/i })).toBeInTheDocument()
    expect(screen.getByTestId('signal-breakdown')).toBeInTheDocument()
  })
})
