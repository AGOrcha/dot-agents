/**
 * trendChart — shared primitives for the aggregate per-run trend charts.
 *
 * ScoreTrendChart and CacheTrendChart plot the same shape: one numeric value
 * per run, ordered oldest→newest and windowed to the most recent N runs, drawn
 * as a Recharts line with a visually-hidden data list mirroring it for screen
 * readers (and for jsdom assertions, where ResponsiveContainer has no
 * measurable box). Only the value selector, colour, and labels differ, so the
 * series builder and the presentation live here once.
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

/** Number of most-recent runs a trend chart windows to by default. */
export const DEFAULT_TREND_WINDOW = 30

export interface TrendPoint {
  /** Stable point key (session id). */
  key: string
  /** X-axis label: last iteration reached, or the session id when unknown. */
  label: string
  /** Charted value in [0,1]. */
  value: number
}

/**
 * Order runs deterministically: RFC3339 UTC `last_update` sorts correctly
 * lexicographically; fall back to a zero-padded iteration number, then the
 * session id, so ordering is stable when timestamps are absent.
 */
function sortKey(run: R2DashboardRunSessionDTO): string {
  if (run.last_update) return `0:${run.last_update}`
  if (run.last_iteration != null) return `1:${String(run.last_iteration).padStart(12, '0')}`
  return `2:${run.session_id}`
}

/**
 * Derive a trend series: pick a value per run via `select` (return null to
 * exclude the run — e.g. missing telemetry or an unscored run), order
 * oldest→newest, keep the most recent `windowSize`, and label each point by its
 * last iteration reached (falling back to the session id).
 */
export function buildTrendSeries(
  runs: readonly R2DashboardRunSessionDTO[],
  select: (run: R2DashboardRunSessionDTO) => number | null,
  windowSize: number = DEFAULT_TREND_WINDOW,
): TrendPoint[] {
  return runs
    .map((run) => ({ run, value: select(run) }))
    .filter((r): r is { run: R2DashboardRunSessionDTO; value: number } => r.value != null)
    .sort((a, b) => sortKey(a.run).localeCompare(sortKey(b.run)))
    .slice(-windowSize)
    .map(({ run, value }) => ({
      key: run.session_id,
      label: run.last_iteration != null ? `#${run.last_iteration}` : run.session_id,
      value,
    }))
}

/** Format a [0,1] value as a whole-percent string (shared axis/tooltip/list format). */
const asPercent = (value: number): string => `${Math.round(value * 100)}%`

interface TrendChartProps {
  /** Ordered, already-windowed series to plot. */
  series: readonly TrendPoint[]
  /** Prefix for the `-chart`/`-empty`/`-data` test ids. */
  testId: string
  /** Figure caption, e.g. "Score trend (last 30 runs)". */
  caption: string
  /** Text shown when the series is empty. */
  emptyText: string
  /** Noun phrase for the chart's aria-label, e.g. "Score trend". */
  ariaLabel: string
  /** Line stroke colour. */
  stroke: string
  className?: string
  /** Value formatter for the axis, tooltip, and data list (defaults to percent). */
  format?: (value: number) => string
}

/** Shared presentation for a windowed per-run trend line chart. */
export function TrendChart({
  series,
  testId,
  caption,
  emptyText,
  ariaLabel,
  stroke,
  className = '',
  format = asPercent,
}: Readonly<TrendChartProps>) {
  return (
    <figure
      data-testid={`${testId}-chart`}
      className={`rounded border border-gray-200 bg-white p-4 ${className}`}
    >
      <figcaption className="mb-2 text-sm font-medium text-gray-700">{caption}</figcaption>
      {series.length === 0 ? (
        <p data-testid={`${testId}-empty`} className="text-sm text-gray-400">
          {emptyText}
        </p>
      ) : (
        <>
          <div role="img" aria-label={`${ariaLabel} across the last ${series.length} run(s)`}>
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={series as TrendPoint[]} margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                <XAxis dataKey="label" tick={{ fontSize: 11 }} />
                <YAxis domain={[0, 1]} tickFormatter={(v) => format(Number(v))} tick={{ fontSize: 11 }} />
                <Tooltip formatter={(v) => format(Number(v))} />
                <Line type="monotone" dataKey="value" stroke={stroke} strokeWidth={2} dot={{ r: 2 }} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <ul className="sr-only" data-testid={`${testId}-data`}>
            {series.map((p) => (
              <li key={p.key}>{`${p.label}: ${format(p.value)}`}</li>
            ))}
          </ul>
        </>
      )}
    </figure>
  )
}
