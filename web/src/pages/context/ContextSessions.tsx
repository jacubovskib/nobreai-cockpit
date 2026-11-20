import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useContextLive } from '../../api/queries'
import type { CardDetail } from '../../api/types'
import { useWithProject } from '../../app/project'
import { SessionContext } from './ContextMeter'
import { RestartCleanDialog } from './RestartCleanDialog'
import { btn, h2, muted, panel } from '../card/shared'

// Sala de controle: o medidor de cada sessão viva; clicar abre o detalhe.
export function ControlRoomContext() {
  const withP = useWithProject()
  const live = useContextLive().data ?? []
  const [open, setOpen] = useState<string | null>(null)
  if (live.length === 0) return null
  return (
    <section aria-labelledby="contexto-t" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <h2 id="contexto-t" style={h2}>Contexto das sessões vivas</h2>
      {live.map((l) => (
        <article key={l.id} aria-label={`Contexto ${l.card_key || l.id}`} style={{ ...panel, gap: 10, borderColor: l.over80 ? '#4D2427' : undefined }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'baseline', gap: 8 }}>
            {l.card_key ? (
              <Link to={withP(`/cards/${l.card_key}`)} style={{ color: 'var(--text)', textDecoration: 'none' }}>
                <span className="mono" style={muted}>{l.card_key}</span> <span style={{ fontWeight: 500 }}>{l.card_title}</span>
              </Link>
            ) : <span style={muted}>sem card</span>}
            <button type="button" style={{ ...btn, minHeight: 32 }} aria-expanded={open === l.id} onClick={() => setOpen(open === l.id ? null : l.id)}>
              {open === l.id ? 'Ocultar detalhes' : 'Ver detalhes'}
            </button>
          </div>
          <SessionContext id={l.id} summary={l} expanded={open === l.id} />
        </article>
      ))}
    </section>
  )
}

// Card: seção "Contexto" das sessões vivas do card, com o detalhe e "Recomeçar limpo".
// O painel de recomeço fica aqui, acima da lista: ao recomeçar, a sessão sai do card e some da lista.
export function CardContext({ card }: { card: CardDetail }) {
  const live = (useContextLive().data ?? []).filter((l) => l.card_key === card.key)
  const [restart, setRestart] = useState<string | null>(null)
  if (live.length === 0 && restart === null) return null
  return (
    <section aria-labelledby="ctx-t" style={panel}>
      <h2 id="ctx-t" style={h2}>Contexto</h2>
      {live.map((l) => (
        <div key={l.id} style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <SessionContext id={l.id} summary={l} expanded />
          <div><button type="button" style={btn} disabled={restart !== null} onClick={() => setRestart(l.id)}>Recomeçar limpo</button></div>
        </div>
      ))}
      {restart !== null && (
        <RestartCleanDialog sessionId={restart} whereStopped={card.where_stopped} whereStoppedAt={card.where_stopped_at} onClose={() => setRestart(null)} />
      )}
    </section>
  )
}
