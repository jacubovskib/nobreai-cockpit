import type { CardDetail } from '../../api/types'
import { h2, muted, panel } from './shared'

export function Decisions({ card }: { card: CardDetail }) {
  const ds = card.content.decisions
  return (
    <section aria-labelledby="dec-t" style={panel}>
      <h2 id="dec-t" style={h2}>Decisões</h2>
      {ds.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhuma decisão registrada.</p>}
      {ds.map((d) => (
        <div key={d.id} style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span>{d.what}</span>
          {d.why && <span style={muted}>Porquê: {d.why}</span>}
        </div>
      ))}
    </section>
  )
}
