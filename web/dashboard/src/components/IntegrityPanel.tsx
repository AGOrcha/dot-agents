/**
 * IntegrityPanel — claimed-vs-observed integrity deltas for two-way signals.
 *
 * Renders IterationDetail.integrity: each observation pairs a role's
 * self-reported claim against the objective observed side. When both sides are
 * present the observation is comparable and delta = observed − claimed; a
 * negative delta is an over-claim (rendered red). Integrity never affects the
 * numeric score — it is a transparency panel only.
 */
import type { R2DashboardIterationDTO } from '../api/types.gen'

export type IntegrityRow = NonNullable<R2DashboardIterationDTO['integrity']>[number]
type Side = IntegrityRow['claimed']

interface IntegrityPanelProps {
  integrity: readonly IntegrityRow[] | undefined
}

const num = (v: number) => v.toFixed(3)

function sideLabel(side: Side): string {
  if (!side.present) return 'absent'
  return side.sub_score == null ? 'present' : num(side.sub_score)
}

function deltaClass(delta: number): string {
  if (delta < 0) return 'font-medium text-red-700' // over-claim
  if (delta > 0) return 'text-green-700'
  return 'text-gray-700'
}

export default function IntegrityPanel({ integrity }: Readonly<IntegrityPanelProps>) {
  if (!integrity || integrity.length === 0) {
    return (
      <p data-testid="integrity-empty" className="text-sm text-gray-400">
        No integrity observations for this iteration.
      </p>
    )
  }

  return (
    <table data-testid="integrity-panel" className="min-w-full border-collapse text-sm">
      <caption className="sr-only">Claimed versus observed integrity</caption>
      <thead>
        <tr className="border-b border-gray-200 text-left text-gray-600">
          <th scope="col" className="px-3 py-2 font-medium">Signal</th>
          <th scope="col" className="px-3 py-2 font-medium">Role</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Claimed</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Observed</th>
          <th scope="col" className="px-3 py-2 text-right font-medium">Delta</th>
        </tr>
      </thead>
      <tbody>
        {integrity.map((row, i) => {
          const overclaim = row.comparable && (row.delta ?? 0) < 0
          return (
            <tr
              key={`${row.signal}-${row.role}-${i}`}
              data-testid={`integrity-row-${row.signal}`}
              data-comparable={row.comparable}
              data-overclaim={overclaim}
              className="border-b border-gray-100"
            >
              <td className="px-3 py-2 font-medium text-gray-900">{row.signal}</td>
              <td className="px-3 py-2 text-gray-700">{row.role}</td>
              <td className="px-3 py-2 text-right tabular-nums">{sideLabel(row.claimed)}</td>
              <td className="px-3 py-2 text-right tabular-nums">{sideLabel(row.observed)}</td>
              <td className="px-3 py-2 text-right tabular-nums">
                {row.comparable && row.delta != null ? (
                  <span className={deltaClass(row.delta)}>
                    {row.delta > 0 ? '+' : ''}
                    {num(row.delta)}
                  </span>
                ) : (
                  <span className="text-gray-400" title="not comparable">
                    —
                  </span>
                )}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
