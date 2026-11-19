import type { CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { useRtkSavings } from '../../api/queries'
import type { RtkActivation } from '../../api/types'
import { useWithProject } from '../../app/project'
import { contextLabel, tokens, ymd } from './data'
import { h2, muted, num, panel, td, th, thead } from '../card/shared'

const variation = (pct: number) => {
  const r = Math.round(pct)
  return r < 0 ? `−${Math.abs(r)}%` : r > 0 ? `+${r}%` : '0%'
}

// "2026-11-14" → "14/11", sem passar por Date (que leria a data como UTC).
const ddmm = (day: string) => {
  const [, m, d] = day.split('-')
  return `${d}/${m}`
}

// Ativo agora: há intervalo e o último não foi fechado.
const activeNow = (acts: RtkActivation[]) => acts.length > 0 && !acts[acts.length - 1].off

// historyNote explica quando o RTK não cobre o período inteiro: o limite de
// 90 dias só vale quando o período começa antes disso.
function historyNote(from: string, oldest: string, now = new Date()): string | null {
  if (!from || !oldest || oldest <= from) return null
  const cutoff = ymd(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 90))
  if (from < cutoff) return 'O período pedido é maior que o histórico: o RTK guarda 90 dias.'
  return `Dados do RTK desde ${ddmm(oldest)}.`
}

const count: CSSProperties = { ...muted, fontSize: 12, marginLeft: 6 }

export function RtkSavings({ from, to, projectId, exact }: { from: string; to: string; projectId: number | null; exact: boolean }) {
  const q = useRtkSavings(from, to, projectId, exact)
  const withP = useWithProject()
  const d = q.data
  const note = d?.gain.available ? historyNote(from, d.gain.oldest_day) : null
  return (
    <section aria-labelledby="rtk-t" style={panel}>
      <h2 id="rtk-t" style={h2}>Economia do RTK</h2>
      {q.isPending && <p className="placeholder">Carregando…</p>}
      {q.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Economia do RTK indisponível agora</p>}
      {d && !d.installed && (
        <p style={{ ...muted, margin: 0 }}>RTK não instalado. <Link to={withP('/config')}>Ver na Configuração</Link></p>
      )}
      {d?.installed && !d.gain.available && <p style={{ ...muted, margin: 0 }}>Economia do RTK indisponível agora</p>}
      {d?.gain.available && (
        <>
          <p style={{ margin: 0 }}>
            {d.gain.commands} comandos · {tokens(d.gain.saved_tokens)} tokens economizados ({Math.round(d.gain.savings_pct)}%)
          </p>
          <p style={{ ...muted, margin: 0 }}>Totais do RTK valem para todos os projetos.</p>
          {note && <p style={{ ...muted, margin: 0 }}>{note}</p>}
        </>
      )}
      {d?.installed && !activeNow(d.activations) && (
        <p style={{ ...muted, margin: 0 }}>RTK não está ativo. <Link to={withP('/config')}>Ativar na Configuração</Link></p>
      )}
      {d && d.compare.length > 0 && (
        <>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
              <thead>
                <tr style={thead}>
                  <th scope="col" style={th}>Chave</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Antes (média/chamada)</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Depois (média/chamada)</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Variação</th>
                </tr>
              </thead>
              <tbody>
                {d.compare.map((c) => (
                  <tr key={c.key} style={{ borderTop: '1px solid var(--border)' }}>
                    <td style={td} className="mono">{contextLabel('Bash', c.key)}</td>
                    <td style={num}>{tokens(c.before_avg)}<span style={count}>n={c.before_count}</span></td>
                    <td style={num}>{tokens(c.after_avg)}<span style={count}>n={c.after_count}</span></td>
                    <td style={num}>{variation(c.change_pct)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p style={{ ...muted, margin: 0 }}>A comparação usa todo o histórico, não só o período.</p>
        </>
      )}
      <p style={{ ...muted, margin: 0 }}>O RTK não comprime cat (lê o arquivo inteiro) nem reescreve sed e python3.</p>
    </section>
  )
}
