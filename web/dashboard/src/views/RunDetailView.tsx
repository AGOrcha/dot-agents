/**
 * RunDetailView — placeholder skeleton for t08.
 *
 * Full implementation in t10: session header, IterationTimeline strip,
 * per-iteration SignalBreakdown, IntegrityPanel.
 *
 * R5 extension slot: RunDetailView is the planned composition point for
 * R5's review-ui label/comment widgets. The sessionId param is the
 * stable key R5 will use to scope its write endpoints.
 */
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'

export default function RunDetailView() {
  const { sessionId = '' } = useParams<{ sessionId: string }>()

  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.run(sessionId),
    queryFn: () => apiFetch<R2DashboardRunSessionDTO>(`/runs/${encodeURIComponent(sessionId)}`),
    enabled: !!sessionId,
  })

  return (
    <section aria-labelledby="run-detail-heading">
      <h1 id="run-detail-heading" className="text-2xl font-semibold text-gray-900 mb-2">
        Run Detail
      </h1>
      <p className="text-sm text-gray-500 mb-4">
        Session: <code className="font-mono">{sessionId || '—'}</code>
      </p>
      {isLoading && <p className="text-gray-500 text-sm">Loading run…</p>}
      {isError && <p className="text-gray-400 text-sm">Run not found or API unavailable.</p>}
      {data && (
        <dl className="text-sm text-gray-700 space-y-1">
          <div>
            <dt className="inline font-medium">Harness: </dt>
            <dd className="inline">{data.harness || '—'}</dd>
          </div>
          <div>
            <dt className="inline font-medium">Iterations: </dt>
            <dd className="inline">{data.iteration_count}</dd>
          </div>
        </dl>
      )}
    </section>
  )
}
