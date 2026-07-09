/**
 * AggregateView — placeholder skeleton for t08.
 *
 * Full implementation in t09: runs grid (sortable/filterable), score-trend
 * chart, cache-hit-rate trend chart, loading skeleton, empty-state.
 *
 * This skeleton:
 *   - renders a heading so smoke tests can assert the route loaded
 *   - shows a loading indicator while the TanStack Query suspense boundary
 *     resolves (the runs fetch will fail against no-backend in test/dev-static mode)
 *   - handles the API error gracefully so the app never white-screens
 */
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

interface RunsEnvelopeList {
  data: R2DashboardRunSessionDTO[]
  meta: { count: number; etag: string }
}

export default function AggregateView() {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.runs,
    queryFn: () => apiFetch<R2DashboardRunSessionDTO[]>('/runs'),
  })

  return (
    <section aria-labelledby="aggregate-heading">
      <h1 id="aggregate-heading" className="text-2xl font-semibold text-gray-900 mb-4">
        Runs
      </h1>
      {isLoading && (
        <p className="text-gray-500 text-sm">Loading runs…</p>
      )}
      {isError && (
        <p className="text-gray-400 text-sm">
          Dashboard API is not reachable. Start the service and refresh.
        </p>
      )}
      {data?.length === 0 && (
        <p className="text-gray-500 text-sm">No runs found.</p>
      )}
      {(data?.length ?? 0) > 0 && (
        <p className="text-gray-700 text-sm">{data!.length} run(s) loaded.</p>
      )}
    </section>
  )
}
