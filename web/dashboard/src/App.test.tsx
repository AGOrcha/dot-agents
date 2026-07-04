import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

// Silence fetch errors in jsdom — the API won't be reachable in unit tests.
// TanStack Query retries are disabled (retry:0) per test client below.
beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify({ error: { code: 'internal', message: 'not reachable' } }), {
      status: 500,
    }),
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

function wrapper(initialPath = '/') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: 0 } },
  })
  return (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('App', () => {
  it('renders the Runs heading at /', async () => {
    render(wrapper('/'))
    expect(await screen.findByRole('heading', { name: /runs/i })).toBeInTheDocument()
  })

  it('renders Run Detail heading at /runs/:sessionId', async () => {
    render(wrapper('/runs/abc-123'))
    expect(await screen.findByRole('heading', { name: /run detail/i })).toBeInTheDocument()
  })

  it('renders Iteration Detail heading at /iterations/:n', async () => {
    render(wrapper('/iterations/5'))
    expect(await screen.findByRole('heading', { name: /iteration detail/i })).toBeInTheDocument()
  })

  it('renders Rubric heading at /rubric', async () => {
    render(wrapper('/rubric'))
    expect(await screen.findByRole('heading', { name: /rubric/i })).toBeInTheDocument()
  })

  it('renders navigation links', () => {
    render(wrapper('/'))
    expect(screen.getByRole('link', { name: /runs/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /rubric/i })).toBeInTheDocument()
  })
})
