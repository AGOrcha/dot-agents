/**
 * IterationTimeline — horizontal strip of per-iteration ticks for one run.
 *
 * One tick per iteration (RunDetail.per_iteration), coloured by score band and
 * ordered by iteration number. Clicking a tick selects that iteration; the
 * RunDetailView wires `onSelect` to route into the per-iteration drill-down.
 *
 * Purely presentational: it takes the lightweight per-iteration score refs and
 * a callback, so it unit-tests without a router or query client.
 */
import type { R2DashboardRunSessionDTO } from '../api/types.gen'
import type { ScoreBand } from './ScoreBadge'

export type TimelineIteration = NonNullable<R2DashboardRunSessionDTO['per_iteration']>[number]

interface IterationTimelineProps {
  iterations: readonly TimelineIteration[]
  onSelect?: (iteration: number) => void
  selected?: number
  className?: string
}

const TICK_COLORS: Record<ScoreBand, string> = {
  excellent: 'bg-green-500 hover:bg-green-600',
  good: 'bg-blue-500 hover:bg-blue-600',
  fair: 'bg-yellow-400 hover:bg-yellow-500',
  poor: 'bg-red-500 hover:bg-red-600',
  unscored: 'bg-gray-300 hover:bg-gray-400',
}

function tickLabel(it: TimelineIteration): string {
  const detail = it.score == null ? it.band : `${Math.round(it.score * 100)}% (${it.band})`
  return `Iteration ${it.iteration}: ${detail}`
}

export default function IterationTimeline({
  iterations,
  onSelect,
  selected,
  className = '',
}: Readonly<IterationTimelineProps>) {
  if (iterations.length === 0) {
    return (
      <p data-testid="timeline-empty" className="text-sm text-gray-400">
        No iterations recorded for this run yet.
      </p>
    )
  }

  const ordered = [...iterations].sort((a, b) => a.iteration - b.iteration)

  return (
    <section
      data-testid="iteration-timeline"
      aria-label="Iteration timeline"
      className={`flex flex-wrap items-end gap-1 ${className}`}
    >
      {ordered.map((it) => {
        const isSelected = selected === it.iteration
        return (
          <button
            key={it.iteration}
            type="button"
            data-testid={`timeline-tick-${it.iteration}`}
            data-band={it.band}
            aria-current={isSelected ? 'true' : undefined}
            aria-label={tickLabel(it)}
            title={tickLabel(it)}
            onClick={() => onSelect?.(it.iteration)}
            className={`h-8 w-4 shrink-0 rounded-sm transition-colors ${TICK_COLORS[it.band]} ${
              isSelected ? 'ring-2 ring-gray-800 ring-offset-1' : ''
            }`}
          />
        )
      })}
    </section>
  )
}
