/**
 * ScoreBadge renders a colour-coded pill for a score band.
 *
 * Bands map to API.md §1.3 error codes / spec D3 band ladder:
 *   excellent (≥0.9)  → green
 *   good      (≥0.75) → blue
 *   fair      (≥0.5)  → yellow/amber
 *   poor      (<0.5)  → red
 *   unscored          → gray (no numeric score)
 *
 * Used on run rows, iteration rows, and the iteration timeline tick.
 * R5 extension: rubric-version mismatch renders an info pill alongside
 * this component (out of scope for t08, extension slot documented here).
 */

export type ScoreBand = 'excellent' | 'good' | 'fair' | 'poor' | 'unscored'

interface ScoreBadgeProps {
  band: ScoreBand
  score?: number | null
  className?: string
}

const BAND_STYLES: Record<ScoreBand, string> = {
  excellent: 'bg-green-100 text-green-800 border-green-200',
  good: 'bg-blue-100 text-blue-800 border-blue-200',
  fair: 'bg-yellow-100 text-yellow-800 border-yellow-200',
  poor: 'bg-red-100 text-red-800 border-red-200',
  unscored: 'bg-gray-100 text-gray-500 border-gray-200',
}

export default function ScoreBadge({ band, score, className = '' }: ScoreBadgeProps) {
  const label =
    band === 'unscored'
      ? 'unscored'
      : score != null
        ? `${band} (${(score * 100).toFixed(0)}%)`
        : band

  return (
    <span
      data-testid="score-badge"
      data-band={band}
      className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-medium ${BAND_STYLES[band]} ${className}`}
    >
      {label}
    </span>
  )
}
