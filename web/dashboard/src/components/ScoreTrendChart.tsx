/**
 * ScoreTrendChart — aggregate score trend across runs.
 *
 * Plots the mean per-iteration score (RunSessionDTO.score) for each scored run,
 * windowed to the most recent `windowSize` runs. Unscored runs (`score === null`)
 * are excluded — missing scores degrade to null per spec R2, never an error.
 *
 * Series building and presentation are shared with CacheTrendChart via
 * `./trendChart`; only the value selector, colour, and labels differ here.
 */
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import { buildTrendSeries, DEFAULT_TREND_WINDOW, TrendChart, type TrendPoint } from './trendChart'

export { DEFAULT_TREND_WINDOW }

/** Derive the score-trend series (exported for focused testing). */
export function buildScoreSeries(
  runs: readonly R2DashboardRunSessionDTO[],
  windowSize: number = DEFAULT_TREND_WINDOW,
): TrendPoint[] {
  return buildTrendSeries(runs, (r) => (r.scored && r.score != null ? r.score : null), windowSize)
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
  return (
    <TrendChart
      series={buildScoreSeries(runs, windowSize)}
      testId="score-trend"
      caption={`Score trend (last ${windowSize} runs)`}
      emptyText="No scored runs yet."
      ariaLabel="Score trend"
      stroke="#2563eb"
      className={className}
    />
  )
}
