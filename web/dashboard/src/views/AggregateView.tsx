/**
 * AggregateView — the dashboard landing view (canonical task t09).
 *
 * Renders GET /api/runs as:
 *   - a sortable, filterable runs grid (RunsGrid), and
 *   - two Recharts time-series: a score trend and a cache-hit-rate trend over
 *     the last N runs (ScoreTrendChart / CacheTrendChart).
 *
 * States handled: a loading skeleton while the TanStack Query resolves, an
 * empty-state when the API returns no runs, and a graceful error state when the
 * API is unreachable. Grid/chart rendering is wrapped in an error boundary so a
 * malformed row never white-screens the whole app.
 */
import { Component, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch, type ApiError } from '../api/client'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import RunsGrid from '../components/RunsGrid'
import ScoreTrendChart from '../components/ScoreTrendChart'
import CacheTrendChart from '../components/CacheTrendChart'

class DashboardErrorBoundary extends Component<{ children: ReactNode }, { hasError: boolean }> {
  state = { hasError: false }

  static getDerivedStateFromError(): { hasError: boolean } {
    return { hasError: true }
  }

  render(): ReactNode {
    if (this.state.hasError) {
      return (
        <p data-testid="aggregate-error-boundary" className="text-sm text-red-600">
          Something went wrong rendering the runs dashboard.
        </p>
      )
    }
    return this.props.children
  }
}

function RunsSkeleton() {
  return (
    <div data-testid="runs-skeleton" aria-hidden="true" className="animate-pulse space-y-3">
      <div className="grid gap-6 md:grid-cols-2">
        <div className="h-56 rounded bg-gray-100" />
        <div className="h-56 rounded bg-gray-100" />
      </div>
      <div className="h-8 w-48 rounded bg-gray-100" />
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="h-6 rounded bg-gray-100" />
      ))}
    </div>
  )
}

export default function AggregateView() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: queryKeys.runs,
    queryFn: () => apiFetch<R2DashboardRunSessionDTO[]>('/runs'),
  })

  const runs = data ?? []

  return (
    <section aria-labelledby="aggregate-heading">
      <h1 id="aggregate-heading" className="mb-4 text-2xl font-semibold text-gray-900">
        Runs
      </h1>

      {isLoading && <RunsSkeleton />}

      {isError && (
        <div data-testid="aggregate-error" className="text-sm text-gray-400">
          <p>Dashboard API is not reachable. Start the service and refresh.</p>
          {(error as ApiError)?.code && (
            <p className="mt-1 font-mono text-xs text-gray-400">Error code: {(error as ApiError).code}</p>
          )}
        </div>
      )}

      {!isLoading && !isError && runs.length === 0 && (
        <p data-testid="aggregate-empty" className="text-sm text-gray-500">
          No runs found.
        </p>
      )}

      {!isLoading && !isError && runs.length > 0 && (
        <DashboardErrorBoundary>
          <p className="sr-only" data-testid="runs-count">
            {runs.length} run(s) loaded.
          </p>
          <div className="mb-6 grid gap-6 md:grid-cols-2">
            <ScoreTrendChart runs={runs} />
            <CacheTrendChart runs={runs} />
          </div>
          <RunsGrid runs={runs} />
        </DashboardErrorBoundary>
      )}
    </section>
  )
}
