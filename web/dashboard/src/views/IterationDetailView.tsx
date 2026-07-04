/**
 * IterationDetailView — placeholder skeleton for t08.
 *
 * Full implementation in t10: full IterationDetail record, SignalBreakdown
 * table, IntegrityPanel, objective process checks, transcript turn count.
 *
 * Deep-linkable per design.md Q6: /iterations/:n loads standalone without
 * a run context. The optional iter_log_dir query param disambiguates n
 * across multiple resolved iter-log roots (API.md §1.6).
 */
import { useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardIterationDTO } from '../api/types.gen'

export default function IterationDetailView() {
  const { n = '' } = useParams<{ n: string }>()
  const [searchParams] = useSearchParams()
  const iterLogDir = searchParams.get('iter_log_dir') ?? ''
  const iterN = Number.parseInt(n, 10)

  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.iteration(iterN, iterLogDir),
    queryFn: () => {
      const qs = iterLogDir ? `?iter_log_dir=${encodeURIComponent(iterLogDir)}` : ''
      return apiFetch<R2DashboardIterationDTO>(`/iterations/${iterN}${qs}`)
    },
    enabled: Number.isFinite(iterN) && iterN >= 1,
  })

  return (
    <section aria-labelledby="iteration-detail-heading">
      <h1 id="iteration-detail-heading" className="text-2xl font-semibold text-gray-900 mb-2">
        Iteration Detail
      </h1>
      <p className="text-sm text-gray-500 mb-4">
        Iteration: <code className="font-mono">{n || '—'}</code>
      </p>
      {isLoading && <p className="text-gray-500 text-sm">Loading iteration…</p>}
      {isError && <p className="text-gray-400 text-sm">Iteration not found or API unavailable.</p>}
      {data && (
        <dl className="text-sm text-gray-700 space-y-1">
          <div>
            <dt className="inline font-medium">Wave: </dt>
            <dd className="inline">{data.wave || '—'}</dd>
          </div>
          <div>
            <dt className="inline font-medium">Task: </dt>
            <dd className="inline">{data.task_id || '—'}</dd>
          </div>
          <div>
            <dt className="inline font-medium">Scored: </dt>
            <dd className="inline">{data.scored ? 'yes' : 'no'}</dd>
          </div>
        </dl>
      )}
    </section>
  )
}
