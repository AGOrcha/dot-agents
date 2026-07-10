/**
 * CacheTrendChart — aggregate cache-hit-rate trend across runs.
 *
 * Plots each run's mean cache-hit-rate (RunSessionDTO.mean_cache_hit_rate,
 * which the schema documents as the field that "feeds the aggregate cache-trend
 * view"), over the same window as ScoreTrendChart: ordered oldest→newest by
 * `last_update` (fallback `last_iteration`, then `session_id`) and limited to
 * the most recent `windowSize` runs. Runs that never captured token telemetry
 * (`mean_cache_hit_rate == null`) are excluded rather than charted as zero.
 *
 * A visually-hidden data list mirrors the Recharts surface for screen readers
 * and for jsdom assertions (ResponsiveContainer has no measurable box there).
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
import { DEFAULT_TREND_WINDOW } from './ScoreTrendChart'

export interface CacheTrendPoint {
  /** Stable point key (session id). */
  key: string
  /** X-axis label: last iteration reached, or the session id when unknown. */
  label: string
  /** Mean cache-hit-rate in [0,1]. */
  rate: number
}

function sortKey(run: R2DashboardRunSessionDTO): string {
  if (run.last_update) return `0:${run.last_update}`
  if (run.last_iteration != null) return `1:${String(run.last_iteration).padStart(12, '0')}`
  return `2:${run.session_id}`
}

/** Derive the cache-hit-rate trend series (exported for focused testing). */
export function buildCacheSeries(
  runs: readonly R2DashboardRunSessionDTO[],
  windowSize: number = DEFAULT_TREND_WINDOW,
): CacheTrendPoint[] {
  return runs
    .filter((r) => r.mean_cache_hit_rate != null)
    .slice()
    .sort((a, b) => sortKey(a).localeCompare(sortKey(b)))
    .slice(-windowSize)
    .map((r) => ({
      key: r.session_id,
      label: r.last_iteration != null ? `#${r.last_iteration}` : r.session_id,
      rate: r.mean_cache_hit_rate as number,
    }))
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
  const series = buildCacheSeries(runs, windowSize)

  return (
    <figure
      data-testid="cache-trend-chart"
      className={`rounded border border-gray-200 bg-white p-4 ${className}`}
    >
      <figcaption className="mb-2 text-sm font-medium text-gray-700">
        Cache-hit rate (last {windowSize} runs)
      </figcaption>
      {series.length === 0 ? (
        <p data-testid="cache-trend-empty" className="text-sm text-gray-400">
          No cache telemetry yet.
        </p>
      ) : (
        <>
          <div role="img" aria-label={`Cache-hit rate across the last ${series.length} run(s)`}>
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={series} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                <XAxis dataKey="label" tick={{ fontSize: 11 }} />
                <YAxis domain={[0, 1]} tickFormatter={(v) => `${Math.round(Number(v) * 100)}%`} tick={{ fontSize: 11 }} />
                <Tooltip formatter={(v) => `${Math.round(Number(v) * 100)}%`} />
                <Line type="monotone" dataKey="rate" stroke="#059669" strokeWidth={2} dot={{ r: 2 }} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <ul className="sr-only" data-testid="cache-trend-data">
            {series.map((p) => (
              <li key={p.key}>{`${p.label}: ${Math.round(p.rate * 100)}%`}</li>
            ))}
          </ul>
        </>
      )}
    </figure>
  )
}
