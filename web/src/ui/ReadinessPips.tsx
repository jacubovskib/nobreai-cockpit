export function ReadinessPips({ met, total }: { met: number; total: number }) {
  return (
    <span
      role="img"
      aria-label={`${met}/${total} para handoff`}
      style={{ display: 'inline-flex', gap: 3, alignItems: 'center' }}
    >
      {Array.from({ length: total }, (_, i) => (
        <span
          key={i}
          aria-hidden="true"
          style={{
            width: 8,
            height: 8,
            borderRadius: 999,
            background: i < met ? 'var(--ok)' : 'transparent',
            border: `1.5px solid ${i < met ? 'var(--ok)' : 'var(--text-2)'}`,
          }}
        />
      ))}
    </span>
  )
}
