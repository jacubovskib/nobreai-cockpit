import type { CardDetail } from '../../api/types'
import { AgentSwatch } from '../../ui/AgentSwatch'
import { OtherWindowTag } from '../../ui/OtherWindowTag'
import { Credits, Money } from '../../ui/Money'
import { Tokens } from '../../ui/Tokens'
import { WorktreeTag } from '../../ui/WorktreeTag'
import { minutesSince } from '../board'
import { h2, muted, panel } from './shared'

const cell = { padding: '8px 10px', textAlign: 'left' as const, whiteSpace: 'nowrap' as const }
const num = { ...cell, textAlign: 'right' as const }

function duration(start: string, end: string) {
  const min = Math.max(0, Math.round((Date.parse(end) - Date.parse(start)) / 60000))
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h ${min % 60} min`
}

export function Sessions({ card, currency, rate }: { card: CardDetail; currency?: string; rate?: number }) {
  const sessions = [...card.sessions].sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at))
  const t = card.totals
  return (
    <section aria-labelledby="ses-t" style={panel}>
      <h2 id="ses-t" style={h2}>Sessões</h2>
      {sessions.length === 0 ? (
        <p style={{ ...muted, margin: 0 }}>Nenhuma sessão ainda.</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ color: 'var(--text-2)' }}>
                <th scope="col" style={cell}>#</th>
                <th scope="col" style={cell}>Agente</th>
                <th scope="col" style={cell}>Worktree</th>
                <th scope="col" style={cell}>Início</th>
                <th scope="col" style={cell}>Duração</th>
                <th scope="col" style={num}>Entrada</th>
                <th scope="col" style={num}>Saída</th>
                <th scope="col" style={num}>Cache</th>
                <th scope="col" style={num}>Custo</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s, i) => {
                const live = card.live?.id === s.id
                const stale = !live && s.card_id === card.id && !s.ended_at && s.pid_dead_at
                return (
                  <tr key={s.id} style={{ borderTop: '1px solid var(--border)' }}>
                    <td style={cell}>{i + 1}</td>
                    <td style={cell}>
                      <AgentSwatch agent={s.agent} />
                      <OtherWindowTag parentId={s.parent_session_id} />
                      {live && <span style={{ color: 'var(--ok)', marginLeft: 8 }}>ao vivo</span>}
                      {stale && <span style={{ color: 'var(--warn)', marginLeft: 8 }}>processo encerrado há {minutesSince(s.pid_dead_at)} min</span>}
                    </td>
                    <td style={cell}>{s.worktree ? <WorktreeTag path={s.worktree} linked={s.worktree_linked} branch={s.branch} /> : <span style={muted}>—</span>}</td>
                    <td style={cell}>{new Date(s.started_at).toLocaleString('pt-BR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}</td>
                    <td style={cell}>{duration(s.started_at, s.ended_at ?? s.last_event_at)}</td>
                    <td style={num}><Tokens n={s.usage.input} /></td>
                    <td style={num}><Tokens n={s.usage.output} /></td>
                    <td style={num}><Tokens n={s.usage.cache_write + s.usage.cache_read} /></td>
                    <td style={num}><Money usd={s.usage.cost_usd} unpriced={s.unpriced} currency={currency} rate={rate} /><Credits credits={s.usage.credits} /></td>
                  </tr>
                )
              })}
            </tbody>
            <tfoot>
              <tr style={{ borderTop: '1px solid var(--border)', fontWeight: 600 }}>
                <th scope="row" colSpan={5} style={cell}>Total do card</th>
                <td style={num}><Tokens n={t.input} /></td>
                <td style={num}><Tokens n={t.output} /></td>
                <td style={num}><Tokens n={t.cache_write + t.cache_read} /></td>
                <td style={num}><Money usd={t.cost_usd} unpriced={t.unpriced} currency={currency} rate={rate} /><Credits credits={t.credits} /></td>
              </tr>
            </tfoot>
          </table>
        </div>
      )}
    </section>
  )
}

