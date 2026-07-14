/**
 * RunDetailView — per-run drill-down (canonical task t10).
 *
 * Header: session id, harness, and mean score (ScoreBadge). Below it, an
 * IterationTimeline horizontal strip renders one tick per iteration coloured by
 * band; clicking a tick routes to the per-iteration IterationDetailView. The
 * resolved iter-log root (iter_log_dir) is threaded onto the drill-down link so
 * non-active historical roots keep disambiguating on the way in (API.md §1.6).
 *
 * R5 extension slot: RunDetailView is the planned composition point for R5's
 * review-ui label/comment widgets. The sessionId param is the stable key R5
 * will use to scope its write endpoints.
 */
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import ScoreBadge from '../components/ScoreBadge'
import IterationTimeline from '../components/IterationTimeline'

export default function RunDetailView() {
  const { sessionId = '' } = useParams<{ sessionId: string }>()
  const [searchParams] = useSearchParams()
  const iterLogDir = searchParams.get('iter_log_dir') ?? ''
  const navigate = useNavigate()

  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.run(sessionId),
    queryFn: () => apiFetch<R2DashboardRunSessionDTO>(`/runs/${encodeURIComponent(sessionId)}`),
    enabled: !!sessionId,
  })

  function openIteration(n: number) {
    // Prefer the URL-carried root, else the run's own resolved root, so the
    // drill-down resolves the same iter-log entry the run was discovered in.
    const dir = iterLogDir || data?.iter_log_dir || ''
    const qs = dir ? `?iter_log_dir=${encodeURIComponent(dir)}` : ''
    navigate(`/iterations/${n}${qs}`)
  }

  return (
    <section aria-labelledby="run-detail-heading">
      <header className="mb-4">
        <h1 id="run-detail-heading" className="mb-2 text-2xl font-semibold text-gray-900">
          Run Detail
        </h1>
        <p className="text-sm text-gray-500">
          Session: <code className="font-mono">{sessionId || '—'}</code>
        </p>
        {data && (
          <dl className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm text-gray-700">
            <div>
              <dt className="inline font-medium">Harness: </dt>
              <dd className="inline">{data.harness || '—'}</dd>
            </div>
            <div>
              <dt className="inline font-medium">Iterations: </dt>
              <dd className="inline tabular-nums">{data.iteration_count}</dd>
            </div>
            <div className="flex items-center gap-2">
              <dt className="font-medium">Mean score:</dt>
              <dd>
                <ScoreBadge band={data.band} score={data.score} />
              </dd>
            </div>
          </dl>
        )}
      </header>

      {isLoading && <p className="text-sm text-gray-500">Loading run…</p>}
      {isError && <p className="text-sm text-gray-400">Run not found or API unavailable.</p>}

      {data && (
        <section aria-label="Iteration timeline" className="mt-2">
          <h2 className="mb-2 text-sm font-semibold text-gray-700">Iterations</h2>
          <IterationTimeline iterations={data.per_iteration ?? []} onSelect={openIteration} />
          {(data.per_iteration?.length ?? 0) > 0 && (
            <p className="mt-2 text-xs text-gray-400">Select a tick to open the iteration detail.</p>
          )}
        </section>
      )}
    </section>
  )
}
