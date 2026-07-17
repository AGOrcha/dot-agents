import { render, screen, within } from '@testing-library/react'
import RubricTable, { type RubricSignal, type RubricBand } from './RubricTable'

const SIGNALS: RubricSignal[] = [
  {
    id: 'landed',
    label: 'Landed',
    weight: 0.5,
    description: 'Whether the change was committed and pushed.',
    two_way: true,
  },
  {
    id: 'tests',
    label: 'Tests',
    weight: 0.25,
    description: 'Whether tests were run and passed.',
    two_way: true,
  },
  {
    id: 'scope',
    label: 'Scope',
    weight: 0.25,
    description: 'Whether the change stayed within scope.',
    two_way: true,
  },
]

// Sorted descending by min, lowest anchored at 0 (matches DTO contract).
const BANDS: RubricBand[] = [
  { name: 'excellent', min: 0.8 },
  { name: 'good', min: 0.6 },
  { name: 'fair', min: 0.4 },
  { name: 'poor', min: 0 },
]

describe('RubricTable', () => {
  it('renders both the signals and the bands tables', () => {
    render(<RubricTable signals={SIGNALS} bands={BANDS} />)
    expect(screen.getByTestId('rubric-signals-table')).toBeInTheDocument()
    expect(screen.getByTestId('rubric-bands-table')).toBeInTheDocument()
  })

  it('renders one row per signal with its label, id, and weight as a percentage', () => {
    render(<RubricTable signals={SIGNALS} bands={BANDS} />)
    expect(screen.getAllByTestId(/^rubric-signal-row-/)).toHaveLength(3)

    const landed = screen.getByTestId('rubric-signal-row-landed')
    expect(within(landed).getByText('Landed')).toBeInTheDocument()
    expect(within(landed).getByText('landed')).toBeInTheDocument()
    expect(within(landed).getByText('50%')).toBeInTheDocument()
    expect(
      within(landed).getByText('Whether the change was committed and pushed.'),
    ).toBeInTheDocument()

    expect(within(screen.getByTestId('rubric-signal-row-tests')).getByText('25%')).toBeInTheDocument()
  })

  it('renders one row per band as an inclusive score range', () => {
    render(<RubricTable signals={SIGNALS} bands={BANDS} />)
    expect(screen.getAllByTestId(/^rubric-band-row-/)).toHaveLength(4)

    // Top band is capped at 1.00; lower bands end at the next-higher band's min.
    expect(within(screen.getByTestId('rubric-band-row-excellent')).getByText('0.80 – 1.00')).toBeInTheDocument()
    expect(within(screen.getByTestId('rubric-band-row-good')).getByText('0.60 – 0.80')).toBeInTheDocument()
    expect(within(screen.getByTestId('rubric-band-row-fair')).getByText('0.40 – 0.60')).toBeInTheDocument()
    // Lowest band is anchored at 0.
    expect(within(screen.getByTestId('rubric-band-row-poor')).getByText('0.00 – 0.40')).toBeInTheDocument()
  })
})
