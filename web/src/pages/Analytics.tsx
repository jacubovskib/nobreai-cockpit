import { useMemo, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { useCards, useSessions, useSettings, useTotals } from '../api/queries'
import type { Total } from '../api/types'
import { agentInfo } from '../ui/agents'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Money } from '../ui/Money'
import { DailyChart } from './analytics/DailyChart'
import { buildDays, range, tokens, usd2, ymd } from './analytics/data'
import { btn, h2, muted, panel } from './card/shared'

const PERIODS = [
  { id: 1, label: 'Hoje', long: 'hoje' },
  { id: 7, label: '7 dias', long: '7 dias' },
  { id: 30, label: '30 dias', long: '30 dias' },
]
const ORDER = ['claude', 'codex', 'kiro']
const th: CSSProperties = { padding: '8px 12px', fontWeight: 500 }
const td: CSSProperties = { padding: '10px 12px' }
const thead: CSSProperties = { textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }
const num: CSSProperties = { ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }

const sum = (ts: Total[], f: (t: Total) => number) => ts.reduce((s, t) => s + f(t), 0)

function duration(startIso: string, endIso: string | null, lastIso: string) {
  const ms = new Date(endIso ?? lastIso).getTime() - new Date(startIso).getTime()
  const min = Math.max(0, Math.round(ms / 60000))
  return min >= 60 ? `${Math.floor(min / 60)} h ${String(min % 60).padStart(2, '0')} min` : `${min} min`
}

const startFmt = new Intl.DateTimeFormat('pt-BR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

export function Analytics() {
  const [period, setPeriod] = useState(7)
  const [asTable, setAsTable] = useState(false)
  const { from, to } = useMemo(() => range(period), [period])
  const settings = useSettings().data
  const cur = { currency: settings?.currency, rate: settings?.brl_rate }
  const dayAgent = useTotals('day_agent', from, to)
  const byAgent = useTotals('agent', from, to)
  const byCard = useTotals('card', from, to)
  const sessions = useSessions(200)
  const cards = useCards().data

  const days = useMemo(() => buildDays(dayAgent.data ?? [], from, to), [dayAgent.data, from, to])
  const agents = useMemo(() => {
    const seen = new Set<string>((dayAgent.data ?? []).map((t) => t.agent))
    const known = ORDER.filter((a) => seen.has(a) || !dayAgent.data?.length)
    return [...known, ...[...seen].filter((a) => !ORDER.includes(a))]
  }, [dayAgent.data])

  const agentRows = byAgent.data ?? []
  const cost = sum(agentRows, (t) => t.cost_usd)
  const costUnpriced = agentRows.some((t) => t.unpriced)
  const inTok = sum(agentRows, (t) => t.input)
  const outTok = sum(agentRows, (t) => t.output)
  const cacheTok = sum(agentRows, (t) => t.cache_write + t.cache_read)
  const nSessions = sum(agentRows, (t) => t.sessions)
  const cardRows = (byCard.data ?? []).filter((t) => t.key !== '')
  const topCards = [...(byCard.data ?? [])].sort((a, b) => b.cost_usd - a.cost_usd).slice(0, 5)
  const topMax = Math.max(0.0001, ...topCards.map((t) => t.cost_usd))
  const titleOf = (key: string) => cards?.find((c) => c.key === key)?.title ?? ''
  const cardKeyOf = (id: number | null) => cards?.find((c) => c.id === id)?.key ?? ''
  const periodSessions = (sessions.data ?? [])
    .filter((s) => ymd(new Date(s.started_at)) >= from && ymd(new Date(s.started_at)) < to)
    .slice(0, 20)
  const long = PERIODS.find((p) => p.id === period)!.long
  const failed = dayAgent.isError || byAgent.isError || byCard.isError || sessions.isError
  const loading = dayAgent.isPending || byAgent.isPending
  const avg = cost / period

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1180 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Analytics</h1>
          <p style={{ ...muted, margin: '4px 0 0' }}>Tokens e custo por sessão, card, agente e provider · preços da tabela em Configuração</p>
        </div>
        <div role="group" aria-label="Período" style={{ display: 'flex', gap: 8 }}>
          {PERIODS.map((p) => (
            <button
              key={p.id}
              type="button"
              aria-pressed={period === p.id}
              onClick={() => setPeriod(p.id)}
              style={{ ...btn, borderRadius: 999, borderColor: period === p.id ? '#4A505D' : '#2E333E', background: period === p.id ? '#2A2F3A' : 'transparent', color: period === p.id ? '#fff' : '#C3C8D2' }}
            >
              {p.label}
            </button>
          ))}
        </div>
      </header>

      {failed && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os dados de uso.</p>}
      {loading && !failed && <p className="placeholder">Carregando…</p>}

      <section aria-label={`Resumo de ${long}`} style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12 }}>
        <div style={panel}>
          <span style={muted}>Custo em {long}</span>
          <span style={{ fontSize: 26, fontWeight: 600 }}><Money usd={cost} unpriced={costUnpriced && cost === 0} {...cur} /></span>
          <span style={muted}>
            {costUnpriced ? 'inclui uso de modelo sem preço, fora da soma' : `média ${cur.currency === 'BRL' && cur.rate ? `R$ ${usd2(avg * cur.rate)}` : `US$ ${usd2(avg)}`}/dia`}
            {cur.currency !== 'BRL' && cur.rate ? ` · ≈ R$ ${usd2(cost * cur.rate)}` : ''}
          </span>
        </div>
        <div style={panel}>
          <span style={muted}>Tokens</span>
          <span style={{ fontSize: 26, fontWeight: 600 }}>{tokens(inTok + outTok + cacheTok)}</span>
          <span style={muted}>{tokens(inTok)} entrada · {tokens(outTok)} saída · {tokens(cacheTok)} cache</span>
        </div>
        <div style={panel}>
          <span style={muted}>Sessões</span>
          <span style={{ fontSize: 26, fontWeight: 600 }}>{nSessions}</span>
          <span style={muted}>
            em {cardRows.length} {cardRows.length === 1 ? 'card' : 'cards'}
            {nSessions > 0 && !costUnpriced ? ` · US$ ${usd2(cost / nSessions)} por sessão` : ''}
          </span>
        </div>
      </section>

      <section aria-labelledby="dia-t" style={panel}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
          <h2 id="dia-t" style={h2}>Custo por dia e por agente (US$)</h2>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
            <ul aria-label="Legenda" style={{ display: 'flex', gap: 16, listStyle: 'none', margin: 0, padding: 0 }}>
              {agents.map((a) => (
                <li key={a}><AgentSwatch agent={a} /></li>
              ))}
            </ul>
            <button type="button" style={btn} aria-pressed={asTable} onClick={() => setAsTable((v) => !v)}>
              {asTable ? 'Ver gráfico' : 'Ver como tabela'}
            </button>
          </div>
        </div>
        {!asTable && <DailyChart days={days} agents={agents} />}
        {asTable && (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
              <thead>
                <tr style={thead}>
                  <th scope="col" style={th}>Dia</th>
                  {agents.map((a) => <th key={a} scope="col" style={{ ...th, textAlign: 'right' }}>{agentInfo(a).name}</th>)}
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Total</th>
                </tr>
              </thead>
              <tbody>
                {days.map((d) => {
                  const tot = Object.values(d.cells).reduce((s, c) => s + c.usd, 0)
                  const unp = Object.values(d.cells).some((c) => c.unpriced)
                  return (
                    <tr key={d.day} style={{ borderTop: '1px solid var(--border)' }}>
                      <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>{d.label}</th>
                      {agents.map((a) => (
                        <td key={a} style={num}>{d.cells[a]?.unpriced ? 'sem preço' : usd2(d.cells[a]?.usd ?? 0)}</td>
                      ))}
                      <td style={num}>{unp ? `${usd2(tot)} + sem preço` : usd2(tot)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 16 }}>
        <section aria-labelledby="ag-t" style={panel}>
          <h2 id="ag-t" style={h2}>Por agente · {long}</h2>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
              <thead>
                <tr style={thead}>
                  <th scope="col" style={th}>Agente</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Sessões</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Tokens</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Custo</th>
                  <th scope="col" style={{ ...th, textAlign: 'right' }}>Por sessão</th>
                </tr>
              </thead>
              <tbody>
                {agentRows.length === 0 && <tr><td colSpan={5} style={{ ...td, color: 'var(--text-2)' }}>Sem uso no período.</td></tr>}
                {agentRows.map((t) => (
                  <tr key={t.key} style={{ borderTop: '1px solid var(--border)' }}>
                    <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}><AgentSwatch agent={t.key} /></th>
                    <td style={num}>{t.sessions}</td>
                    <td style={num}>{tokens(t.input + t.output + t.cache_write + t.cache_read)}</td>
                    <td style={num}><Money usd={t.cost_usd} unpriced={t.unpriced} {...cur} /></td>
                    <td style={num}>{t.unpriced || t.sessions === 0 ? '—' : usd2(t.cost_usd / t.sessions)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section aria-labelledby="cards-t" style={panel}>
          <h2 id="cards-t" style={h2}>Cards mais caros · US$</h2>
          {topCards.length === 0 && <p style={muted}>Sem uso no período.</p>}
          {topCards.map((t) => {
            const label = t.key === '' ? 'sem card' : t.key
            const inner = (
              <>
                <span className="mono" style={{ color: 'var(--text-2)', minWidth: 64 }}>{label}</span>
                <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.key === '' ? 'Uso fora de cards' : titleOf(t.key)}</span>
                  <span style={{ display: 'block', height: 6, borderRadius: 3, background: '#2A2F3A' }}>
                    <span style={{ display: 'block', height: 6, borderRadius: 3, background: '#C3C8D2', width: `${t.unpriced ? 0 : Math.round((t.cost_usd / topMax) * 100)}%` }} />
                  </span>
                </span>
                <span style={{ fontVariantNumeric: 'tabular-nums' }}>{t.unpriced ? 'sem preço' : usd2(t.cost_usd)}</span>
              </>
            )
            const row: CSSProperties = { display: 'flex', alignItems: 'center', gap: 12, color: 'inherit', textDecoration: 'none', minHeight: 40 }
            return t.key === '' ? <div key="" style={row}>{inner}</div> : <Link key={t.key} to={`/cards/${t.key}`} style={row}>{inner}</Link>
          })}
        </section>
      </div>

      <section aria-labelledby="ses-t" style={panel}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 12 }}>
          <h2 id="ses-t" style={h2}>Sessões recentes</h2>
          <span style={muted}>Cada linha é uma sessão capturada pelos hooks</span>
        </div>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
            <thead>
              <tr style={thead}>
                <th scope="col" style={th}>Início</th>
                <th scope="col" style={th}>Card</th>
                <th scope="col" style={th}>Agente</th>
                <th scope="col" style={th}>Modelo</th>
                <th scope="col" style={th}>Duração</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Entrada</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Saída</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Cache</th>
                <th scope="col" style={{ ...th, textAlign: 'right' }}>Custo</th>
              </tr>
            </thead>
            <tbody>
              {periodSessions.length === 0 && <tr><td colSpan={9} style={{ ...td, color: 'var(--text-2)' }}>Nenhuma sessão no período.</td></tr>}
              {periodSessions.map((s) => {
                const key = cardKeyOf(s.card_id)
                const live = !s.ended_at && !s.pid_dead_at
                return (
                  <tr key={s.id} style={{ borderTop: '1px solid var(--border)' }}>
                    <td style={td}>{startFmt.format(new Date(s.started_at))}</td>
                    <td style={td}>{key ? <Link to={`/cards/${key}`}>{key}</Link> : '—'}</td>
                    <td style={td}><AgentSwatch agent={s.agent} /></td>
                    <td style={td}>{s.model || '—'}</td>
                    <td style={td}>{duration(s.started_at, s.ended_at, s.last_event_at)}{live ? ' · ao vivo' : ''}</td>
                    <td style={num}>{tokens(s.usage.input)}</td>
                    <td style={num}>{tokens(s.usage.output)}</td>
                    <td style={num}>{tokens(s.usage.cache_write + s.usage.cache_read)}</td>
                    <td style={num}><Money usd={s.usage.cost_usd} unpriced={s.unpriced} {...cur} /></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </section>
      <p style={muted}>Custo calculado pela tabela de preços por modelo; em planos de assinatura, mostra o equivalente em API. Uso de modelo sem preço aparece como “sem preço”, nunca como zero.</p>
    </div>
  )
}
