import type { CardDetail } from '../../api/types'
import { TASK_LABEL } from '../../ui/PlanBlocks'
import { ddmm } from '../projects'
import { h2, muted, panel } from './shared'

export function Plan({ card }: { card: CardDetail }) {
  const plan = card.content.plan
  return (
    <section aria-labelledby="plan-t" style={panel}>
      <h2 id="plan-t" style={h2}>Plano</h2>
      {!plan || plan.tasks.length === 0 ? (
        <p style={{ ...muted, margin: 0 }}>Sem plano registrado. O agente registra com nobre plan.</p>
      ) : (
        <>
          <span className="mono" style={muted}>{plan.path}</span>
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {plan.tasks.map((t) => (
              <li key={t.n} style={{ display: 'flex', gap: 10, alignItems: 'baseline', flexWrap: 'wrap' }}>
                <span className="mono" style={muted}>{t.n}</span>
                <span style={{ color: t.status === 'concluida' ? 'var(--text-2)' : 'var(--text)' }}>{t.title}</span>
                <span style={muted}>{TASK_LABEL[t.status]}</span>
                <span style={muted}>{ddmm(t.updated_at)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
