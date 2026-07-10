/**
 * IterationDetailView — per-iteration full detail (canonical task t10).
 *
 * Renders the full IterationDetail record (task/commit/files/lines/retries plus
 * per-verifier outcomes), the SignalBreakdown table (weight · sub-score ·
 * contribution per rubric signal from PersistedScore.Breakdown), and the
 * IntegrityPanel (claimed-vs-observed deltas for two-way signals).
 *
 * Deep-linkable per design.md Q6: /iterations/:n loads this view standalone
 * without a run context. The optional iter_log_dir query param disambiguates n
 * across multiple resolved iter-log roots (API.md §1.6).
 */
import type { ReactNode } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '../api/queryKeys'
import { apiFetch } from '../api/client'
import type { R2DashboardIterationDTO } from '../api/types.gen'
import ScoreBadge from '../components/ScoreBadge'
import SignalBreakdown from '../components/SignalBreakdown'
import IntegrityPanel from '../components/IntegrityPanel'

type VerifierResult = NonNullable<R2DashboardIterationDTO['verifiers']>[number]

const VERIFIER_STATUS_CLASS: Record<VerifierResult['status'], string> = {
  pass: 'text-green-700',
  fail: 'text-red-700',
  partial: 'text-yellow-700',
  unknown: 'text-gray-500',
}

function Field({ label, children }: Readonly<{ label: string; children: ReactNode }>) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-gray-400">{label}</dt>
      <dd className="text-gray-800">{children}</dd>
    </div>
  )
}

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
      <header className="mb-4">
        <h1 id="iteration-detail-heading" className="mb-2 text-2xl font-semibold text-gray-900">
          Iteration Detail
        </h1>
        <p className="text-sm text-gray-500">
          Iteration: <code className="font-mono">{n || '—'}</code>
          {data && <ScoreBadge band={data.band} score={data.score} className="ml-2 align-middle" />}
        </p>
      </header>

      {isLoading && <p className="text-sm text-gray-500">Loading iteration…</p>}
      {isError && <p className="text-sm text-gray-400">Iteration not found or API unavailable.</p>}

      {data && (
        <div className="space-y-8">
          <section aria-label="Iteration record">
            <h2 className="mb-2 text-sm font-semibold text-gray-700">Record</h2>
            <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
              <Field label="Task">{data.task_id || '—'}</Field>
              <Field label="Wave">{data.wave || '—'}</Field>
              <Field label="Commit">
                <code className="font-mono text-xs">{data.commit || '—'}</code>
              </Field>
              <Field label="Files changed">{data.files_changed ?? '—'}</Field>
              <Field label="Lines added">
                {data.lines_added == null ? '—' : `+${data.lines_added}`}
              </Field>
              <Field label="Lines removed">
                {data.lines_removed == null ? '—' : `-${data.lines_removed}`}
              </Field>
              <Field label="Retries">{data.retries ?? '—'}</Field>
              <Field label="Scored">{data.scored ? 'yes' : 'no'}</Field>
              <Field label="Rubric">{data.rubric_version || '—'}</Field>
            </dl>
          </section>

          {data.verifiers && data.verifiers.length > 0 && (
            <section aria-label="Verifier results">
              <h2 className="mb-2 text-sm font-semibold text-gray-700">Verifiers</h2>
              <ul className="space-y-1 text-sm">
                {data.verifiers.map((v, i) => (
                  <li
                    key={`${v.type}-${i}`}
                    data-testid={`verifier-${v.type}`}
                    className="flex flex-wrap items-center gap-x-3 text-gray-700"
                  >
                    <span className="font-medium text-gray-900">{v.type}</span>
                    <span className={VERIFIER_STATUS_CLASS[v.status]}>{v.status}</span>
                    {typeof v.tests_added === 'number' && (
                      <span className="text-gray-500">+{v.tests_added} tests</span>
                    )}
                    {v.gate_passed != null && (
                      <span className="text-gray-500">gate {v.gate_passed ? 'passed' : 'failed'}</span>
                    )}
                    {typeof v.retries === 'number' && v.retries > 0 && (
                      <span className="text-gray-500">{v.retries} retries</span>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}

          <section aria-label="Signal breakdown">
            <h2 className="mb-2 text-sm font-semibold text-gray-700">Signal breakdown</h2>
            <SignalBreakdown breakdown={data.breakdown} />
          </section>

          <section aria-label="Integrity">
            <h2 className="mb-2 text-sm font-semibold text-gray-700">Integrity (claimed vs observed)</h2>
            <IntegrityPanel integrity={data.integrity} />
          </section>
        </div>
      )}
    </section>
  )
}
