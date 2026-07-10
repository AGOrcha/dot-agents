/**
 * ScoreTrendChart — aggregate score trend across runs.
 *
 * Plots the mean per-iteration score (RunSessionDTO.score) for each scored run,
 * ordered oldest→newest by `last_update` (fallback `last_iteration`, then
 * `session_id`) and limited to the most recent `windowSize` runs. The x-axis is
 * keyed by the run's last iteration reached so the series reads as a
 * score-per-iteration trend across the fleet. Unscored runs (`score === null`)
 * are excluded — missing scores degrade to null per spec R2, never an error.
 *
 * A visually-hidden data list mirrors the Recharts surface so the trend is
 * accessible to screen readers (and assertable in jsdom, where Recharts'
 * ResponsiveContainer has no measurable box).
 */
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

export const DEFAULT_TREND_WINDOW = 30

export interface ScoreTrendPoint {
  /** Stable point key (session id). */
  key: string
  /** X-axis label: last iteration reached, or the session id when unknown. */
  label: string
  /** Mean per-iteration score in [0,1]. */
  score: number
}

function sortKey(run: R2DashboardRunSessionDTO): string {
  // RFC3339 UTC timestamps sort correctly lexicographically; fall back to a
  // zero-padded iteration number, then the session id, so ordering is stable.
  if (run.last_update) return `0:${run.last_update}`
  if (run.last_iteration != null) return `1:${String(run.last_iteration).padStart(12, '0')}`
  return `2:${run.session_id}`
}

/** Derive the score-trend series (exported for focused testing). */
export function buildScoreSeries(
  runs: readonly R2DashboardRunSessionDTO[],
  windowSize: number = DEFAULT_TREND_WINDOW,
): ScoreTrendPoint[] {
  return runs
    .filter((r) => r.scored && r.score != null)
    .slice()
    .sort((a, b) => sortKey(a).localeCompare(sortKey(b)))
    .slice(-windowSize)
    .map((r) => ({
      key: r.session_id,
      label: r.last_iteration != null ? `#${r.last_iteration}` : r.session_id,
      score: r.score as number,
    }))
}

interface ScoreTrendChartProps {
  runs: readonly R2DashboardRunSessionDTO[]
  windowSize?: number
  className?: string
}

export default function ScoreTrendChart({
  runs,
  windowSize = DEFAULT_TREND_WINDOW,
  className = '',
}: Readonly<ScoreTrendChartProps>) {
  const series = buildScoreSeries(runs, windowSize)

  return (
    <figure
      data-testid="score-trend-chart"
      className={`rounded border border-gray-200 bg-white p-4 ${className}`}
    >
      <figcaption className="mb-2 text-sm font-medium text-gray-700">
        Score trend (last {windowSize} runs)
      </figcaption>
      {series.length === 0 ? (
        <p data-testid="score-trend-empty" className="text-sm text-gray-400">
          No scored runs yet.
        </p>
      ) : (
        <>
          <div role="img" aria-label={`Score trend across the last ${series.length} run(s)`}>
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={series} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                <XAxis dataKey="label" tick={{ fontSize: 11 }} />
                <YAxis domain={[0, 1]} tickFormatter={(v) => `${Math.round(Number(v) * 100)}%`} tick={{ fontSize: 11 }} />
                <Tooltip formatter={(v) => `${Math.round(Number(v) * 100)}%`} />
                <Line type="monotone" dataKey="score" stroke="#2563eb" strokeWidth={2} dot={{ r: 2 }} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <ul className="sr-only" data-testid="score-trend-data">
            {series.map((p) => (
              <li key={p.key}>{`${p.label}: ${Math.round(p.score * 100)}%`}</li>
            ))}
          </ul>
        </>
      )}
    </figure>
  )
}
