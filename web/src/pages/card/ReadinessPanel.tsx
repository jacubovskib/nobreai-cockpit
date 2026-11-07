import type { ReadinessResult } from '../../api/types'
import { h2, muted, panel } from './shared'

export function ReadinessPanel({ readiness }: { readiness: ReadinessResult }) {
  return (
    <section aria-labelledby="ready-t" style={panel}>
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <h2 id="ready-t" style={h2}>Pronto para handoff?</h2>
        <span style={muted}>{readiness.met} de {readiness.total}</span>
      </div>
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
        {readiness.items.map((r) => (
          <li key={r.rule} style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {r.ok ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#0CA30C" strokeWidth="2.4" role="img" aria-label="Cumprido"><path d="M5 12.5l4.5 4.5L19 7" /></svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#FAB219" strokeWidth="2.4" role="img" aria-label="Faltando"><circle cx="12" cy="12" r="8" /><path d="M12 8v5M12 16v.5" /></svg>
            )}
            <span>{r.label}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
