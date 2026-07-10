import { render, screen, within } from '@testing-library/react'
import SignalBreakdown, { type BreakdownRow } from './SignalBreakdown'

const ROWS: BreakdownRow[] = [
  // present, effective === nominal → no nominal annotation
  {
    signal: 'landed',
    label: 'Landed',
    present: true,
    sub_score: 0.75,
    nominal_weight: 0.2,
    effective_weight: 0.2,
    contribution: 0.15,
  },
  // absent → data-present false, sub-score em-dash, renormalized effective weight
  {
    signal: 'tests',
    label: 'Tests',
    present: false,
    sub_score: 0,
    nominal_weight: 0.25,
    effective_weight: 0,
    contribution: 0,
    detail: 'no tests detected',
  },
  // present, effective !== nominal → nominal annotation shown
  {
    signal: 'scope',
    label: 'Scope',
    present: true,
    sub_score: 0.6,
    nominal_weight: 0.25,
    effective_weight: 0.3,
    contribution: 0.18,
  },
]

describe('SignalBreakdown', () => {
  it('renders the unscored placeholder for undefined and empty breakdowns', () => {
    const { rerender } = render(<SignalBreakdown breakdown={undefined} />)
    expect(screen.getByTestId('breakdown-empty')).toHaveTextContent(
      'No signal breakdown — this iteration is unscored.',
    )
    expect(screen.queryByTestId('signal-breakdown')).not.toBeInTheDocument()

    rerender(<SignalBreakdown breakdown={[]} />)
    expect(screen.getByTestId('breakdown-empty')).toBeInTheDocument()
  })

  it('renders one row per signal', () => {
    render(<SignalBreakdown breakdown={ROWS} />)
    expect(screen.getByTestId('signal-breakdown')).toBeInTheDocument()
    expect(screen.getAllByTestId(/^breakdown-row-/)).toHaveLength(3)
  })

  it('shows weight %, sub-score.toFixed(3) and contribution for a present row', () => {
    render(<SignalBreakdown breakdown={ROWS} />)
    const row = screen.getByTestId('breakdown-row-landed')
    expect(row).toHaveAttribute('data-present', 'true')
    expect(within(row).getByText('20%')).toBeInTheDocument()
    expect(within(row).getByText('0.750')).toBeInTheDocument()
    expect(within(row).getByText('0.150')).toBeInTheDocument()
  })

  it('marks an absent row with data-present="false", an em-dash sub-score and an absent label', () => {
    render(<SignalBreakdown breakdown={ROWS} />)
    const row = screen.getByTestId('breakdown-row-tests')
    expect(row).toHaveAttribute('data-present', 'false')
    expect(within(row).getByText('—')).toBeInTheDocument()
    expect(within(row).getByText('absent')).toBeInTheDocument()
    expect(within(row).getByText('no tests detected')).toBeInTheDocument()
  })

  it('renders the nominal-weight annotation only when effective weight differs from nominal', () => {
    render(<SignalBreakdown breakdown={ROWS} />)
    // effective === nominal → no annotation
    expect(within(screen.getByTestId('breakdown-row-landed')).queryByText(/nominal/)).toBeNull()
    // effective (30%) !== nominal (25%) → annotation present
    const scope = screen.getByTestId('breakdown-row-scope')
    expect(within(scope).getByText('30%')).toBeInTheDocument()
    expect(within(scope).getByText(/\(nominal 25%\)/)).toBeInTheDocument()
  })
})
