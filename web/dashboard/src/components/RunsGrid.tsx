/**
 * RunsGrid — sortable, filterable table of run-session summaries.
 *
 * Columns (spec t09): session id, harness, last iter, iters count, mean score,
 * band, last update. Rows come straight from GET /api/runs
 * (R2DashboardRunSessionDTO[]). Clicking a column header sorts by that column
 * (toggling asc/desc); the free-text filter narrows by session id or harness.
 * Nulls always sort last regardless of direction so unscored / telemetry-less
 * runs never crowd the top. Band is rendered via the shared ScoreBadge.
 */
import { useMemo, useState } from 'react'
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import ScoreBadge, { type ScoreBand } from './ScoreBadge'

type SortKey =
  | 'session_id'
  | 'harness'
  | 'last_iteration'
  | 'iteration_count'
  | 'score'
  | 'band'
  | 'last_update'

type SortDir = 'asc' | 'desc'

interface Column {
  key: SortKey
  label: string
  /** Right-align numeric columns. */
  numeric?: boolean
}

const COLUMNS: readonly Column[] = [
  { key: 'session_id', label: 'Session' },
  { key: 'harness', label: 'Harness' },
  { key: 'last_iteration', label: 'Last iter', numeric: true },
  { key: 'iteration_count', label: 'Iters', numeric: true },
  { key: 'score', label: 'Mean score', numeric: true },
  { key: 'band', label: 'Band' },
  { key: 'last_update', label: 'Last update' },
]

const BAND_RANK: Record<ScoreBand, number> = {
  excellent: 4,
  good: 3,
  fair: 2,
  poor: 1,
  unscored: 0,
}

/** Extract the comparable value for a column; `null` sorts last. */
function sortValue(run: R2DashboardRunSessionDTO, key: SortKey): number | string | null {
  switch (key) {
    case 'session_id':
      return run.session_id
    case 'harness':
      return run.harness || null
    case 'last_iteration':
      return run.last_iteration ?? null
    case 'iteration_count':
      return run.iteration_count
    case 'score':
      return run.score
    case 'band':
      return BAND_RANK[run.band]
    case 'last_update':
      return run.last_update ?? null
  }
}

/** Compare two runs on `key`/`dir`, with nulls pinned last in both directions. */
export function compareRuns(
  a: R2DashboardRunSessionDTO,
  b: R2DashboardRunSessionDTO,
  key: SortKey,
  dir: SortDir,
): number {
  const av = sortValue(a, key)
  const bv = sortValue(b, key)
  if (av == null && bv == null) return 0
  if (av == null) return 1
  if (bv == null) return -1
  let base: number
  if (typeof av === 'number' && typeof bv === 'number') {
    base = av - bv
  } else {
    base = String(av).localeCompare(String(bv))
  }
  return dir === 'asc' ? base : -base
}

function formatScore(score: number | null): string {
  return score == null ? '—' : `${Math.round(score * 100)}%`
}

function formatUpdate(ts: string | null | undefined): string {
  if (!ts) return '—'
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString()
}

function ariaSortValue(active: boolean, sortDir: SortDir): 'ascending' | 'descending' | 'none' {
  if (!active) return 'none'
  return sortDir === 'asc' ? 'ascending' : 'descending'
}

function sortIndicator(active: boolean, sortDir: SortDir): string {
  if (!active) return ''
  return sortDir === 'asc' ? '▲' : '▼'
}

interface RunsGridProps {
  runs: readonly R2DashboardRunSessionDTO[]
  initialSortKey?: SortKey
  initialSortDir?: SortDir
}

export default function RunsGrid({
  runs,
  initialSortKey = 'last_update',
  initialSortDir = 'desc',
}: Readonly<RunsGridProps>) {
  const [filter, setFilter] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>(initialSortKey)
  const [sortDir, setSortDir] = useState<SortDir>(initialSortDir)

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const filtered = needle
      ? runs.filter(
          (r) =>
            r.session_id.toLowerCase().includes(needle) ||
            (r.harness ?? '').toLowerCase().includes(needle),
        )
      : runs
    return filtered.slice().sort((a, b) => compareRuns(a, b, sortKey, sortDir))
  }, [runs, filter, sortKey, sortDir])

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  return (
    <div data-testid="runs-grid">
      <input
        type="search"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Filter by session or harness…"
        aria-label="Filter runs"
        data-testid="runs-filter"
        className="mb-3 w-full max-w-sm rounded border border-gray-300 px-3 py-1.5 text-sm"
      />
      <div className="overflow-x-auto">
        <table className="min-w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-gray-200 text-left text-gray-600">
              {COLUMNS.map((col) => {
                const active = col.key === sortKey
                return (
                  <th
                    key={col.key}
                    scope="col"
                    aria-sort={ariaSortValue(active, sortDir)}
                    className={`px-3 py-2 font-medium ${col.numeric ? 'text-right' : 'text-left'}`}
                  >
                    <button
                      type="button"
                      onClick={() => toggleSort(col.key)}
                      data-testid={`sort-${col.key}`}
                      className="inline-flex items-center gap-1 hover:text-gray-900"
                    >
                      {col.label}
                      <span aria-hidden="true" className="text-xs">
                        {sortIndicator(active, sortDir)}
                      </span>
                    </button>
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr>
                <td colSpan={COLUMNS.length} className="px-3 py-6 text-center text-gray-400" data-testid="runs-grid-no-match">
                  No runs match “{filter}”.
                </td>
              </tr>
            ) : (
              visible.map((run) => (
                <tr
                  key={run.session_id}
                  data-testid={`run-row-${run.session_id}`}
                  className="border-b border-gray-100 hover:bg-gray-50"
                >
                  <td className="px-3 py-2 font-mono text-xs text-gray-900">{run.session_id}</td>
                  <td className="px-3 py-2 text-gray-700">{run.harness || '—'}</td>
                  <td className="px-3 py-2 text-right tabular-nums text-gray-700">
                    {run.last_iteration ?? '—'}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums text-gray-700">{run.iteration_count}</td>
                  <td className="px-3 py-2 text-right tabular-nums text-gray-700">{formatScore(run.score)}</td>
                  <td className="px-3 py-2">
                    <ScoreBadge band={run.band} score={run.score} />
                  </td>
                  <td className="px-3 py-2 text-gray-500">{formatUpdate(run.last_update)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
