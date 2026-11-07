import { COLUMN_LABELS } from './agents'

export function StatusBadge({ column, live }: { column: string; live?: boolean }) {
  const label = COLUMN_LABELS[column] ?? column
  if (live) {
    return (
      <span className="badge badge-live">
        <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true">
          <circle cx="4" cy="4" r="3.5" fill="var(--ok)" />
        </svg>
        Ao vivo · {label}
      </span>
    )
  }
  return <span className="badge">{label}</span>
}
