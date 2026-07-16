/**
 * RubricView — active rubric explainer (canonical task t15).
 *
 * Fetches GET /api/rubric (the presentation projection of the active
 * outcome-scoring rubric, R2DashboardRubricDTO) and renders it: the rubric
 * version + combination method as a header, then the signal weights and the
 * score-band ladder via RubricTable. The rubric is immutable per process, so the
 * query uses a long stale time. Loading and error states are handled inline;
 * every score in the UI links here for an explanation of how it was computed.
 */
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch, type ApiError } from '../api/client'
import type { R2DashboardRubricDTO } from '../api/types.gen'
import RubricTable from '../components/RubricTable'

export default function RubricView() {
  const { data, isLoading, isError, error } = useQuery<R2DashboardRubricDTO, ApiError>({
    queryKey: queryKeys.rubric,
    queryFn: () => apiFetch<R2DashboardRubricDTO>('/rubric'),
    // Rubric is immutable per process; 10 min stale time.
    staleTime: 10 * 60 * 1000,
  })

  return (
    <section aria-labelledby="rubric-heading">
      <h1 id="rubric-heading" className="mb-4 text-2xl font-semibold text-gray-900">
        Rubric
      </h1>

      {isLoading && (
        <p data-testid="rubric-loading" className="text-sm text-gray-500">
          Loading rubric…
        </p>
      )}

      {isError && (
        <p data-testid="rubric-error" className="text-sm text-red-700">
          Rubric unavailable: {error?.message ?? 'unknown error'}
        </p>
      )}

      {data && (
        <div className="space-y-6">
          <p className="text-sm text-gray-600">
            Version <code className="font-mono text-gray-900">{data.version}</code>
            {' · '}
            combination: <code className="font-mono text-gray-900">{data.combination}</code>
          </p>
          <RubricTable signals={data.signals} bands={data.bands} />
        </div>
      )}
    </section>
  )
}
