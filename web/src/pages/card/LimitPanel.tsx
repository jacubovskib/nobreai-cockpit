import type { CardDetail, Limit } from '../../api/types'
import { Money } from '../../ui/Money'
import { h2, muted, panel } from './shared'

// Limite deste card: a API só tem `card_usd` por agente. Usa o limite do agente
// da sessão viva ou, sem ela, da sessão mais recente. 0 (ou sem limite
// cadastrado) = "sem limite".
export function cardLimit(card: CardDetail, limits: Limit[]): number {
  const last = [...card.sessions].sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at)).at(-1)
  const agent = card.live?.agent ?? last?.agent
  return limits.find((l) => l.agent === agent)?.card_usd ?? 0
}

export function LimitPanel({ card, limits, currency, rate }: { card: CardDetail; limits: Limit[]; currency?: string; rate?: number }) {
  const limit = cardLimit(card, limits)
  const spent = card.totals.cost_usd
  const unpriced = card.totals.unpriced
  const pct = limit > 0 ? Math.round((spent / limit) * 100) : 0
  return (
    <section aria-labelledby="lim-t" style={panel}>
      <h2 id="lim-t" style={h2}>Limite deste card</h2>
      {limit > 0 ? (
        <>
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span>
              <strong><Money usd={spent} unpriced={unpriced} currency={currency} rate={rate} /></strong>{' '}
              <span style={muted}>de <Money usd={limit} currency={currency} rate={rate} /></span>
            </span>
            {!unpriced && <span style={muted}>{pct}%</span>}
          </div>
          <div role="progressbar" aria-label="Gasto do card no limite" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.min(pct, 100)} style={{ height: 8, borderRadius: 999, background: 'var(--border)', overflow: 'hidden' }}>
            <div style={{ width: `${Math.min(pct, 100)}%`, height: '100%', background: pct >= 100 ? 'var(--err)' : 'var(--ok)' }} />
          </div>
        </>
      ) : (
        <span>
          <strong><Money usd={spent} unpriced={unpriced} currency={currency} rate={rate} /></strong>{' '}
          <span style={muted}>· sem limite</span>
        </span>
      )}
    </section>
  )
}
