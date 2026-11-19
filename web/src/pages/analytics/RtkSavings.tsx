import { Link } from 'react-router-dom'
import { useRtkSavings } from '../../api/queries'
import { contextLabel, tokens } from './data'
import { h2, muted, num, panel, td, th, thead } from '../card/shared'

const variation = (pct: number) => {
  const r = Math.round(pct)
  return r < 0 ? `−${Math.abs(r)}%` : r > 0 ? `+${r}%` : '0%'
}

export function RtkSavings({ from, to, projectId, exact }: { from: string; to: string; projectId: number | null; exact: boolean }) {
  const q = useRtkSavings(from, to, projectId, exact)
  const d = q.data
  return (
    <section aria-labelledby="rtk-t" style={panel}>
      <h2 id="rtk-t" style={h2}>Economia do RTK</h2>
      {q.isPending && <p className="placeholder">Carregando…</p>}
      {q.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Economia do RTK indisponível agora</p>}
      {d && !d.gain.available && <p style={{ ...muted, margin: 0 }}>Economia do RTK indisponível agora</p>}
      {d?.gain.available && (
        <>
          <p style={{ margin: 0 }}>
            {d.gain.commands} comandos · {tokens(d.gain.saved_tokens)} tokens economizados ({Math.round(d.gain.savings_pct)}%)
          </p>
          <p style={{ ...muted, margin: 0 }}>Totais do RTK valem para todos os projetos.</p>
          {d.gain.oldest_day > from && <p style={{ ...muted, margin: 0 }}>O período pedido é maior que o histórico: o RTK guarda 90 dias.</p>}
        </>
      )}
      {d && d.activations.length === 0 && (
        <p style={{ ...muted, margin: 0 }}>RTK não está ativo. <Link to="/config">Ativar na Configuração</Link></p>
      )}
      {d && d.compare.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
            <thead>
              <tr style={thead}>
                <th scope="col" style={th}>Chave</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Antes</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Depois</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Variação</th>
              </tr>
            </thead>
            <tbody>
              {d.compare.map((c) => (
                <tr key={c.key} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={td} className="mono">{contextLabel('Bash', c.key)}</td>
                  <td style={num}>{tokens(c.before_avg)}</td>
                  <td style={num}>{tokens(c.after_avg)}</td>
                  <td style={num}>{variation(c.change_pct)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p style={{ ...muted, margin: 0 }}>O RTK não comprime cat (lê o arquivo inteiro) nem reescreve sed e python3.</p>
    </section>
  )
}
