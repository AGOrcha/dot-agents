import { render, screen, within } from '@testing-library/react'
import IntegrityPanel, { type IntegrityRow } from './IntegrityPanel'

const ROWS: IntegrityRow[] = [
  // comparable over-claim: observed below claimed → negative delta
  {
    signal: 'landed',
    role: 'impl',
    claimed: { present: true, sub_score: 0.9 },
    observed: { present: true, sub_score: 0.65 },
    comparable: true,
    delta: -0.25,
  },
  // not comparable: claim absent, delta null → em-dash
  {
    signal: 'tests',
    role: 'verifier',
    claimed: { present: false },
    observed: { present: true, sub_score: 0.5 },
    comparable: false,
    delta: null,
  },
  // comparable positive delta, claimed present with no sub_score → "present"
  {
    signal: 'scope',
    role: 'review',
    claimed: { present: true },
    observed: { present: true, sub_score: 0.7 },
    comparable: true,
    delta: 0.1,
  },
]

describe('IntegrityPanel', () => {
  it('renders the empty placeholder for undefined and empty integrity', () => {
    const { rerender } = render(<IntegrityPanel integrity={undefined} />)
    expect(screen.getByTestId('integrity-empty')).toHaveTextContent(
      'No integrity observations for this iteration.',
    )
    expect(screen.queryByTestId('integrity-panel')).not.toBeInTheDocument()

    rerender(<IntegrityPanel integrity={[]} />)
    expect(screen.getByTestId('integrity-empty')).toBeInTheDocument()
  })

  it('renders one row per observation with signal and role labels', () => {
    render(<IntegrityPanel integrity={ROWS} />)
    expect(screen.getByTestId('integrity-panel')).toBeInTheDocument()
    expect(screen.getAllByTestId(/^integrity-row-/)).toHaveLength(3)
    const row = screen.getByTestId('integrity-row-landed')
    expect(within(row).getByText('landed')).toBeInTheDocument()
    expect(within(row).getByText('impl')).toBeInTheDocument()
  })

  it('flags a comparable over-claim with data-overclaim="true" and a negative delta value', () => {
    render(<IntegrityPanel integrity={ROWS} />)
    const row = screen.getByTestId('integrity-row-landed')
    expect(row).toHaveAttribute('data-comparable', 'true')
    expect(row).toHaveAttribute('data-overclaim', 'true')
    expect(within(row).getByText('-0.250')).toBeInTheDocument()
  })

  it('renders "—" for a non-comparable observation and does not flag it as an over-claim', () => {
    render(<IntegrityPanel integrity={ROWS} />)
    const row = screen.getByTestId('integrity-row-tests')
    expect(row).toHaveAttribute('data-comparable', 'false')
    expect(row).toHaveAttribute('data-overclaim', 'false')
    expect(within(row).getByText('—')).toBeInTheDocument()
  })

  it('prefixes a positive delta with "+" and never flags it as over-claim', () => {
    render(<IntegrityPanel integrity={ROWS} />)
    const row = screen.getByTestId('integrity-row-scope')
    expect(row).toHaveAttribute('data-overclaim', 'false')
    expect(within(row).getByText('+0.100')).toBeInTheDocument()
  })

  it('labels claimed/observed sides: absent, numeric sub-score, and bare "present"', () => {
    render(<IntegrityPanel integrity={ROWS} />)
    // claimed absent → "absent"; observed numeric → "0.500"
    const tests = screen.getByTestId('integrity-row-tests')
    expect(within(tests).getByText('absent')).toBeInTheDocument()
    expect(within(tests).getByText('0.500')).toBeInTheDocument()
    // claimed present with no sub_score → "present"; observed numeric → "0.700"
    const scope = screen.getByTestId('integrity-row-scope')
    expect(within(scope).getByText('present')).toBeInTheDocument()
    expect(within(scope).getByText('0.700')).toBeInTheDocument()
    // both sides numeric on the over-claim row
    const landed = screen.getByTestId('integrity-row-landed')
    expect(within(landed).getByText('0.900')).toBeInTheDocument()
    expect(within(landed).getByText('0.650')).toBeInTheDocument()
  })
})
