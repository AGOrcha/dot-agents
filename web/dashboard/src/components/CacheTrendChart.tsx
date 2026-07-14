/**
 * CacheTrendChart — aggregate cache-hit-rate trend across runs.
 *
 * Plots each run's mean cache-hit-rate (RunSessionDTO.mean_cache_hit_rate,
 * which the schema documents as the field that "feeds the aggregate cache-trend
 * view"), over the same window as ScoreTrendChart. Runs that never captured
 * token telemetry (`mean_cache_hit_rate == null`) are excluded rather than
 * charted as zero.
 *
 * Series building and presentation are shared with ScoreTrendChart via
 * `./trendChart`; only the value selector, colour, and labels differ here.
 */
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import { buildTrendSeries, DEFAULT_TREND_WINDOW, TrendChart, type TrendPoint } from './trendChart'

/** Derive the cache-hit-rate trend series (exported for focused testing). */
export function buildCacheSeries(
  runs: readonly R2DashboardRunSessionDTO[],
  windowSize: number = DEFAULT_TREND_WINDOW,
): TrendPoint[] {
  return buildTrendSeries(runs, (r) => r.mean_cache_hit_rate ?? null, windowSize)
}

interface CacheTrendChartProps {
  runs: readonly R2DashboardRunSessionDTO[]
  windowSize?: number
  className?: string
}

export default function CacheTrendChart({
  runs,
  windowSize = DEFAULT_TREND_WINDOW,
  className = '',
}: Readonly<CacheTrendChartProps>) {
  return (
    <TrendChart
      series={buildCacheSeries(runs, windowSize)}
      testId="cache-trend"
      caption={`Cache-hit rate (last ${windowSize} runs)`}
      emptyText="No cache telemetry yet."
      ariaLabel="Cache-hit rate"
      stroke="#059669"
      className={className}
    />
  )
}
