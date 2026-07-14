/**
 * SignalBreakdown — per-signal score decomposition table for one iteration.
 *
 * One row per rubric signal from PersistedScore.Breakdown (already in rubric
 * order). Columns: signal label, effective weight, sub-score, contribution.
 * Absent signals (present:false) contribute 0 and render muted; the present
 * contributions sum to the iteration's numeric score. Empty/undefined
 * breakdown (an unscored iteration) renders an explanatory placeholder.
 */
import type { R2DashboardIterationDTO } from '../api/types.gen'

export type BreakdownRow = NonNullable<R2DashboardIterationDTO['breakdown']>[number]

interface SignalBreakdownProps {
  breakdown: readonly BreakdownRow[] | undefined
}

const pct = (v: number) => `${Math.round(v * 100)}%`
const num = (v: number) => v.toFixed(3)

export default function SignalBreakdown({ breakdown }: Readonly<SignalBreakdownProps>) {
  if (!breakdown || breakdown.length === 0) {
    return (
      <p data-testid="breakdown-empty" className="text-sm text-gray-400">
        No signal breakdown — this iteration is unscored.
      </p>
    )
  }

  return (
    <table data-testid="signal-breakdown" className="min-w-full border-collapse text-sm">
      <caption className="sr-only">Per-signal score breakdown</caption>
      <thead>
        <tr className="border-b border-gray-200 text-left text-gray-600">
          <th scope="col" className="px-3 py-2 font-medium">Signal</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Weight</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Sub-score</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Contribution</th>
        </tr>
      </thead>
      <tbody>
        {breakdown.map((row) => (
          <tr
            key={row.signal}
            data-testid={`breakdown-row-${row.signal}`}
            data-present={row.present}
            className={`border-b border-gray-100 ${row.present ? '' : 'text-gray-400'}`}
          >
            <td className="px-3 py-2">
              <span className="font-medium text-gray-900">{row.label}</span>
              {!row.present && <span className="ml-2 text-xs italic text-gray-400">absent</span>}
              {row.detail && <span className="block text-xs text-gray-500">{row.detail}</span>}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
              {pct(row.effective_weight)}
              {row.effective_weight !== row.nominal_weight && (
                <span className="ml-1 text-xs text-gray-400">(nominal {pct(row.nominal_weight)})</span>
              )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
              {row.present ? num(row.sub_score) : '—'}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">{num(row.contribution)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
