/**
 * RubricView — placeholder skeleton for t08.
 *
 * Full implementation in t15: active rubric explainer (version, six signals
 * with weight + label + description, band ladder), per-iteration rubric-version
 * mismatch pill. Each score in the UI links here.
 */
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardRubricDTO } from '../api/types.gen'

export default function RubricView() {
  const { data, isLoading, isError } = useQuery({
    queryKey: queryKeys.rubric,
    queryFn: () => apiFetch<R2DashboardRubricDTO>('/rubric'),
    // Rubric is immutable per process; 10 min stale time.
    staleTime: 10 * 60 * 1000,
  })

  return (
    <section aria-labelledby="rubric-heading">
      <h1 id="rubric-heading" className="text-2xl font-semibold text-gray-900 mb-4">
        Rubric
      </h1>
      {isLoading && <p className="text-gray-500 text-sm">Loading rubric…</p>}
      {isError && <p className="text-gray-400 text-sm">Rubric unavailable.</p>}
      {data && (
        <div>
          <p className="text-sm text-gray-600 mb-4">
            Version <code className="font-mono text-gray-900">{data.version}</code>
            {' · '}
            combination: <code className="font-mono text-gray-900">{data.combination}</code>
          </p>
          <table className="w-full text-sm border-collapse">
            <thead>
              <tr className="bg-gray-100 text-left">
                <th className="px-3 py-2 font-medium text-gray-700">Signal</th>
                <th className="px-3 py-2 font-medium text-gray-700">Weight</th>
                <th className="px-3 py-2 font-medium text-gray-700">Description</th>
              </tr>
            </thead>
            <tbody>
              {data.signals.map((sig) => (
                <tr key={sig.id} className="border-t border-gray-200">
                  <td className="px-3 py-2 font-mono">{sig.id}</td>
                  <td className="px-3 py-2">{(sig.weight * 100).toFixed(0)}%</td>
                  <td className="px-3 py-2 text-gray-600">{sig.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
