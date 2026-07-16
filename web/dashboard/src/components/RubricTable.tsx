/**
 * RubricTable — presentational table for the active outcome-scoring rubric.
 *
 * Renders two sections of the R2DashboardRubricDTO:
 *   - the signal set with its nominal weights + descriptions (weights sum to 1.0
 *     and are shown as percentages), and
 *   - the score-band ladder, sorted descending by min, as inclusive score ranges.
 *
 * The DTO's bands carry only a lower bound (`min`); the upper bound of each band
 * is the min of the next-higher band (or 1.0 for the top band), so the ladder is
 * rendered as `[min, max]` ranges. Purely presentational — no data fetching.
 */
import type { R2DashboardRubricDTO } from '../api/types.gen'

export type RubricSignal = R2DashboardRubricDTO['signals'][number]
export type RubricBand = R2DashboardRubricDTO['bands'][number]

interface RubricTableProps {
  signals: readonly RubricSignal[]
  bands: readonly RubricBand[]
}

const pct = (weight: number) => `${(weight * 100).toFixed(0)}%`
const bound = (v: number) => v.toFixed(2)

export default function RubricTable({ signals, bands }: Readonly<RubricTableProps>) {
  return (
    <div className="space-y-8">
      <div>
        <h2 className="mb-2 text-lg font-medium text-gray-900">Signals &amp; weights</h2>
        <table data-testid="rubric-signals-table" className="min-w-full border-collapse text-sm">
          <caption className="sr-only">Rubric signals and their nominal weights</caption>
          <thead>
            <tr className="border-b border-gray-200 text-left text-gray-600">
              <th scope="col" className="px-3 py-2 font-medium">Signal</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Weight</th>
              <th scope="col" className="px-3 py-2 font-medium">Description</th>
            </tr>
          </thead>
          <tbody>
            {signals.map((sig) => (
              <tr
                key={sig.id}
                data-testid={`rubric-signal-row-${sig.id}`}
                className="border-b border-gray-100"
              >
                <td className="px-3 py-2 font-medium text-gray-900">
                  {sig.label}
                  <span className="ml-2 font-mono text-xs text-gray-400">{sig.id}</span>
                </td>
                <td className="px-3 py-2 text-right tabular-nums">{pct(sig.weight)}</td>
                <td className="px-3 py-2 text-gray-600">{sig.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div>
        <h2 className="mb-2 text-lg font-medium text-gray-900">Score bands</h2>
        <table data-testid="rubric-bands-table" className="min-w-full border-collapse text-sm">
          <caption className="sr-only">Score-band ladder</caption>
          <thead>
            <tr className="border-b border-gray-200 text-left text-gray-600">
              <th scope="col" className="px-3 py-2 font-medium">Band</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Score range</th>
            </tr>
          </thead>
          <tbody>
            {bands.map((band, i) => {
              // Bands are sorted descending by min; a band's upper bound is the
              // next-higher band's min, and the top band is capped at 1.0.
              const max = i === 0 ? 1 : bands[i - 1].min
              return (
                <tr
                  key={band.name}
                  data-testid={`rubric-band-row-${band.name}`}
                  className="border-b border-gray-100"
                >
                  <td className="px-3 py-2 font-medium capitalize text-gray-900">{band.name}</td>
                  <td className="px-3 py-2 text-right tabular-nums">
                    {bound(band.min)} – {bound(max)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
