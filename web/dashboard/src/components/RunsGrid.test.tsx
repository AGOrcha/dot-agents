import { render, screen, within, fireEvent } from '@testing-library/react'
import RunsGrid, { compareRuns } from './RunsGrid'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

function run(over: Partial<R2DashboardRunSessionDTO> & { session_id: string }): R2DashboardRunSessionDTO {
  return {
    rubric_version: '2.1.0',
    iteration_count: 1,
    scored: true,
    score: 0.5,
    band: 'fair',
    ...over,
  }
}

const RUNS: R2DashboardRunSessionDTO[] = [
  run({ session_id: 'alpha', harness: 'claude-code', last_iteration: 3, iteration_count: 3, score: 0.92, band: 'excellent', last_update: '2026-03-01T10:00:00Z' }),
  run({ session_id: 'bravo', harness: 'codex', last_iteration: 8, iteration_count: 8, score: 0.4, band: 'poor', last_update: '2026-03-02T10:00:00Z' }),
  run({ session_id: 'charlie', harness: 'cursor', last_iteration: null, iteration_count: 0, scored: false, score: null, band: 'unscored', last_update: null }),
]

function rowSessionOrder(): string[] {
  return screen
    .getAllByTestId(/^run-row-/)
    .map((tr) => tr.getAttribute('data-testid')!.replace('run-row-', ''))
}

describe('compareRuns', () => {
  it('sorts numeric columns ascending', () => {
    const sorted = [...RUNS].sort((a, b) => compareRuns(a, b, 'iteration_count', 'asc'))
    expect(sorted.map((r) => r.session_id)).toEqual(['charlie', 'alpha', 'bravo'])
  })

  it('pins null values last even when descending', () => {
    const sorted = [...RUNS].sort((a, b) => compareRuns(a, b, 'score', 'desc'))
    // charlie has null score → last despite desc
    expect(sorted[sorted.length - 1].session_id).toBe('charlie')
    expect(sorted[0].session_id).toBe('alpha')
  })

  it('ranks bands by the ladder, not alphabetically', () => {
    const sorted = [...RUNS].sort((a, b) => compareRuns(a, b, 'band', 'desc'))
    expect(sorted.map((r) => r.band)).toEqual(['excellent', 'poor', 'unscored'])
  })
})

describe('RunsGrid', () => {
  it('renders one row per run with its band badge', () => {
    render(<RunsGrid runs={RUNS} />)
    expect(screen.getAllByTestId(/^run-row-/)).toHaveLength(3)
    const alphaRow = screen.getByTestId('run-row-alpha')
    expect(within(alphaRow).getByTestId('score-badge')).toHaveAttribute('data-band', 'excellent')
    expect(within(alphaRow).getByText('92%')).toBeInTheDocument()
  })

  it('filters rows by session id or harness (case-insensitive)', () => {
    render(<RunsGrid runs={RUNS} />)
    fireEvent.change(screen.getByTestId('runs-filter'), { target: { value: 'CODEX' } })
    expect(rowSessionOrder()).toEqual(['bravo'])
  })

  it('shows a no-match message when the filter excludes everything', () => {
    render(<RunsGrid runs={RUNS} />)
    fireEvent.change(screen.getByTestId('runs-filter'), { target: { value: 'zzz' } })
    expect(screen.getByTestId('runs-grid-no-match')).toBeInTheDocument()
    expect(screen.queryAllByTestId(/^run-row-/)).toHaveLength(0)
  })

  it('sorts by a column when its header is clicked and toggles direction', () => {
    render(<RunsGrid runs={RUNS} initialSortKey="last_update" initialSortDir="desc" />)
    // Click "Session" header → ascending by session id.
    fireEvent.click(screen.getByTestId('sort-session_id'))
    expect(rowSessionOrder()).toEqual(['alpha', 'bravo', 'charlie'])
    // Click again → descending.
    fireEvent.click(screen.getByTestId('sort-session_id'))
    expect(rowSessionOrder()).toEqual(['charlie', 'bravo', 'alpha'])
  })

  it('reflects the active sort column via aria-sort', () => {
    render(<RunsGrid runs={RUNS} />)
    fireEvent.click(screen.getByTestId('sort-iteration_count'))
    const header = screen.getByTestId('sort-iteration_count').closest('th')!
    expect(header).toHaveAttribute('aria-sort', 'ascending')
    expect(screen.getByTestId('sort-session_id').closest('th')).toHaveAttribute('aria-sort', 'none')

    fireEvent.click(screen.getByTestId('sort-iteration_count'))
    expect(header).toHaveAttribute('aria-sort', 'descending')
  })
})
