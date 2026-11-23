import { useState, type CSSProperties } from 'react'
import { useContextSession, useContextTop, useProjects } from '../../api/queries'
import { h2, muted, num, panel, td, th, thead } from '../card/shared'
import { ContextCurve } from './ContextCurve'
import { contextLabel, tokens } from './data'

const right: CSSProperties = { ...th, textAlign: 'right' }
const table: CSSProperties = { width: '100%', borderCollapse: 'collapse', fontSize: 14 }
const startFmt = new Intl.DateTimeFormat('pt-BR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

function SessionDetail({ id }: { id: string }) {
  const q = useContextSession(id)
  if (q.isPending) return <p style={muted}>Carregando…</p>
  if (q.isError) return <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar a sessão.</p>
  const turns = q.data.turns ?? []
  const top = q.data.top ?? []
  const pctOnly = turns.length > 0 && turns.every((t) => t.pct_only)
  return (
    <div style={{ padding: '8px 12px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
      {pctOnly && <span style={muted}>janela informada pelo agente</span>}
      <ContextCurve turns={turns} />
      <h3 style={{ fontSize: 14, fontWeight: 600, margin: 0 }}>Saídas mais pesadas</h3>
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
        {top.map((o, i) => (
          <li key={i} style={{ display: 'flex', flexWrap: 'wrap', gap: 12, fontSize: 14 }}>
            <span className="mono" style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }}>{contextLabel(o.tool, o.key)}</span>
            {o.subagent && <span style={muted}>subagente</span>}
            <span style={{ ...muted, fontVariantNumeric: 'tabular-nums' }}>{tokens(o.tokens + o.in_tokens)} × {o.rereads} releituras</span>
            <span style={{ fontVariantNumeric: 'tabular-nums' }}>{tokens(o.weight)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// ContextSection: o que enche o contexto do agente (peso = tamanho × releituras).
export function ContextSection({ from, to, projectId, exact }: { from: string; to: string; projectId: number | null; exact: boolean }) {
  const q = useContextTop(from, to, projectId, exact)
  const projects = useProjects().data
  const [open, setOpen] = useState<string | null>(null)
  const keys = q.data?.keys ?? []
  const sessions = q.data?.sessions ?? []
  const byProject = q.data?.projects ?? []
  const weightTotal = q.data?.weight_total ?? 0
  // Chaves fora da lista (além do top 30) entram no peso total; sem esta
  // linha o peso delas não apareceria em lugar nenhum.
  const others = Math.max(0, weightTotal - keys.reduce((s, k) => s + k.weight, 0))
  const rest = Math.max(0, (q.data?.context_total ?? 0) - weightTotal)
  const nameOf = (id: number) => projects?.find((p) => p.id === id)?.name ?? `projeto ${id}`

  return (
    <section aria-labelledby="ctx-t" style={panel}>
      <h2 id="ctx-t" style={h2}>O que enche o contexto</h2>
      <p style={{ ...muted, margin: 0 }}>Peso = tamanho × turnos em que a saída foi relida até a próxima compactação.</p>
      <p style={{ ...muted, margin: 0 }}>Estimativa: bytes ÷ 4. O resto inclui prompt de sistema, ferramentas, skills, mensagens e respostas.</p>
      {q.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar o contexto.</p>}
      {q.isPending && <p style={muted}>Carregando…</p>}
      {q.data && keys.length === 0 && <p style={muted}>Nenhuma saída medida no período.</p>}
      {q.data && keys.length > 0 && (
        <>
          <div style={{ overflowX: 'auto' }}>
            <table style={table}>
              <thead>
                <tr style={thead}>
                  <th scope="col" style={th}>Chave</th>
                  <th scope="col" style={right}>Chamadas</th>
                  <th scope="col" style={right}>Entrada</th>
                  <th scope="col" style={right}>Saída</th>
                  <th scope="col" style={right}>Média</th>
                  <th scope="col" style={right}>Peso</th>
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={`${k.tool}|${k.key}`} style={{ borderTop: '1px solid var(--border)' }}>
                    <th scope="row" className="mono" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>{contextLabel(k.tool, k.key)}</th>
                    <td style={num}>{k.count}</td>
                    <td style={num}>{tokens(k.in_tokens)}</td>
                    <td style={num}>{tokens(k.tokens)}</td>
                    <td style={num}>{tokens(k.avg_tokens)}</td>
                    <td style={num}>{tokens(k.weight)}</td>
                  </tr>
                ))}
                {others > 0 && (
                  <tr style={{ borderTop: '1px solid var(--border)', color: 'var(--text-2)' }}>
                    <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>Outras chaves</th>
                    <td style={num} /><td style={num} /><td style={num} /><td style={num} />
                    <td style={num}>{tokens(others)}</td>
                  </tr>
                )}
                <tr style={{ borderTop: '1px solid var(--border)', color: 'var(--text-2)' }}>
                  <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>Resto (base e conversa)</th>
                  <td style={num} /><td style={num} /><td style={num} /><td style={num} />
                  <td style={num}>{tokens(rest)}</td>
                </tr>
              </tbody>
            </table>
          </div>

          {projectId === null && byProject.length > 0 && (
            <>
              <h3 style={{ fontSize: 14, fontWeight: 600, margin: '8px 0 0' }}>Por projeto</h3>
              <div style={{ overflowX: 'auto' }}>
                <table style={table}>
                  <thead>
                    <tr style={thead}>
                      <th scope="col" style={th}>Projeto</th>
                      <th scope="col" style={right}>Peso</th>
                      <th scope="col" style={th}>Chave que mais pesou</th>
                    </tr>
                  </thead>
                  <tbody>
                    {byProject.map((p) => (
                      <tr key={p.project_id} style={{ borderTop: '1px solid var(--border)' }}>
                        <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>{nameOf(p.project_id)}</th>
                        <td style={num}>{tokens(p.weight)}</td>
                        <td className="mono" style={td}>{contextLabel(p.top_tool, p.top_key)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}

          {sessions.length > 0 && (
            <>
              <h3 style={{ fontSize: 14, fontWeight: 600, margin: '8px 0 0' }}>Sessões que mais pesaram</h3>
              <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                {sessions.map((s) => {
                  const expanded = open === s.session_id
                  return (
                    <li key={s.session_id} style={{ borderTop: '1px solid var(--border)' }}>
                      <button
                        type="button"
                        aria-expanded={expanded}
                        onClick={() => setOpen(expanded ? null : s.session_id)}
                        style={{ all: 'unset', boxSizing: 'border-box', width: '100%', cursor: 'pointer', display: 'flex', flexWrap: 'wrap', gap: 12, padding: '10px 12px', fontSize: 14 }}
                      >
                        <span className="mono" style={{ color: 'var(--text-2)', minWidth: 64 }}>{s.card_key || 'sem card'}</span>
                        <span style={{ flex: 1, minWidth: 0 }}>{s.card_title}</span>
                        <span style={muted}>{startFmt.format(new Date(s.started_at))}</span>
                        <span style={{ fontVariantNumeric: 'tabular-nums' }}>peso {tokens(s.weight)}</span>
                        {s.peak_context > 0 && <span style={{ ...muted, fontVariantNumeric: 'tabular-nums' }}>pico {tokens(s.peak_context)}</span>}
                        <span style={muted}>{s.compactions} {s.compactions === 1 ? 'compactação' : 'compactações'}</span>
                      </button>
                      {expanded && <SessionDetail id={s.session_id} />}
                    </li>
                  )
                })}
              </ul>
            </>
          )}
        </>
      )}
    </section>
  )
}
