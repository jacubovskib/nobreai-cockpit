import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useOverview, useSettings } from '../api/queries'
import { patch, post } from '../api/client'
import type { ActiveSession, Alert, Card, DomainEvent, SessionWithUsage, Total } from '../api/types'
import { agentInfo } from '../ui/agents'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Money } from '../ui/Money'
import { Tokens, formatTokens } from '../ui/Tokens'
import { minutesSince } from './board'

const panel: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, padding: 18, borderRadius: 12, background: 'var(--surface)', border: '1px solid var(--border)' }
const btn: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 40, padding: '0 14px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer', textDecoration: 'none' }
const btnPrimary: React.CSSProperties = { ...btn, border: 0, background: '#2A2F3A', color: '#fff' }
const h2: React.CSSProperties = { fontSize: 15, fontWeight: 600 }

const totalTokens = (t: Total) => t.input + t.output + t.cache_write + t.cache_read

function Metric({ label, value, sub, subColor }: { label: string; value: React.ReactNode; sub: React.ReactNode; subColor?: string }) {
  return (
    <div style={{ ...panel, gap: 6 }}>
      <span style={{ fontSize: 12, color: 'var(--text-2)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>{label}</span>
      <span style={{ fontSize: 30, fontWeight: 600, letterSpacing: '-0.02em' }}>{value}</span>
      <span style={{ fontSize: 13, color: subColor ?? 'var(--text-2)' }}>{sub}</span>
    </div>
  )
}

function AlertRow({ alert, onPause, pausing }: { alert: Alert; onPause: (key: string) => void; pausing: boolean }) {
  const red = alert.kind === 'incomplete'
  return (
    <div
      style={{
        display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '12px 16px', padding: '14px 16px', borderRadius: 10,
        background: red ? '#1F1416' : '#1E1A12', border: `1px solid ${red ? '#4D2427' : '#4A3B17'}`,
      }}
    >
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke={red ? 'var(--err)' : 'var(--warn)'} strokeWidth="2" aria-hidden="true"><path d="M12 3 2 21h20z" /><path d="M12 10v5M12 18v.5" /></svg>
      <div style={{ flex: '1 1 320px', minWidth: 0 }}><strong style={{ fontWeight: 600 }}>{alert.text}</strong></div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {alert.kind === 'stale' && alert.card_key && (
          <button type="button" style={btn} disabled={pausing} onClick={() => onPause(alert.card_key)}>Mover para Pausado</button>
        )}
        {(alert.kind === 'limit_warn' || alert.kind === 'unpriced') && <Link to="/config" style={btn}>Ajustar limite</Link>}
        {alert.card_key && <Link to={`/cards/${alert.card_key}`} style={btnPrimary}>Abrir card</Link>}
      </div>
    </div>
  )
}

function ActiveCard({ s }: { s: ActiveSession }) {
  const mins = minutesSince(s.started_at)
  return (
    <article style={{ ...panel, gap: 14 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <strong><AgentSwatch agent={s.agent} /></strong>
          <span className="mono" style={{ fontSize: 12, color: 'var(--text-2)' }}>{s.model}</span>
        </div>
        <span className="badge badge-live">
          <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="3.5" fill="var(--ok)" /></svg>
          Ao vivo · {mins} min
        </span>
      </div>
      {s.card ? (
        <Link to={`/cards/${s.card.key}`} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 8, textDecoration: 'none', color: 'var(--text)' }}>
          <span className="mono" style={{ fontSize: 13, color: 'var(--text-2)' }}>{s.card.key}</span>
          <span style={{ fontSize: 16, fontWeight: 500 }}>{s.card.title}</span>
        </Link>
      ) : (
        <span style={{ color: 'var(--text-2)' }}>sem card</span>
      )}
      {s.last_activity && (
        <div className="mono" style={{ padding: '10px 12px', borderRadius: 8, background: '#0F1116', fontSize: 12.5, color: '#C3C8D2' }}>{s.last_activity}</div>
      )}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 24px', fontSize: 13, color: 'var(--text-2)' }}>
        <span>Sessão <strong style={{ color: 'var(--text)', fontWeight: 500 }}><Tokens n={totalTokens(s.session_usage)} /></strong> tokens</span>
        <span>Custo da sessão <strong style={{ color: 'var(--text)', fontWeight: 500 }}><Money usd={s.session_usage.cost_usd} unpriced={s.session_usage.unpriced} /></strong></span>
        {s.card && <span>Card acumulado <strong style={{ color: 'var(--text)', fontWeight: 500 }}><Money usd={s.card_totals.cost_usd} unpriced={s.card_totals.unpriced} /></strong></span>}
        {s.readiness && <span>Handoff <strong style={{ color: 'var(--text)', fontWeight: 500 }}>{s.readiness.met} de {s.readiness.total} itens</strong></span>}
      </div>
    </article>
  )
}

function NoCardRow({ s }: { s: SessionWithUsage }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [error, setError] = useState('')
  const create = useMutation({
    mutationFn: async () => {
      if (!s.project_id) throw new Error('Esta sessão não pertence a um projeto cadastrado.')
      const card = await post<Card>('/api/cards', { title: title.trim(), project_id: s.project_id })
      await post(`/api/cards/${card.key}/attach`, { session_id: s.id })
    },
    onSuccess: () => {
      setOpen(false); setTitle(''); setError('')
      qc.invalidateQueries({ queryKey: ['overview'] }); qc.invalidateQueries({ queryKey: ['cards'] })
    },
    onError: (e: Error) => setError(e.message),
  })
  const inputId = `titulo-${s.id}`
  const dir = s.cwd.split('/').filter(Boolean).slice(-2).join('/')
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderRadius: 8, background: '#0F1116' }}>
      <span style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, fontSize: 13 }}>
        <AgentSwatch agent={s.agent} /> · <span className="mono">{dir || s.cwd}</span> · {minutesSince(s.started_at)} min ·{' '}
        <Money usd={s.usage.cost_usd} unpriced={s.usage.unpriced} />
      </span>
      {open ? (
        <form
          onSubmit={(e) => { e.preventDefault(); if (title.trim()) create.mutate() }}
          style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'flex-end' }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, flex: '1 1 200px' }}>
            <label htmlFor={inputId} style={{ fontSize: 12, color: 'var(--text-2)' }}>Título do card</label>
            <input
              id={inputId}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              style={{ minHeight: 40, padding: '0 10px', borderRadius: 8, border: '1px solid #2E333E', background: 'var(--surface)', color: 'var(--text)', font: 'inherit' }}
            />
          </div>
          <button type="submit" style={btnPrimary} disabled={create.isPending || !title.trim()}>Criar e vincular</button>
          <button type="button" style={btn} onClick={() => { setOpen(false); setError('') }}>Cancelar</button>
        </form>
      ) : (
        <div><button type="button" style={btn} onClick={() => setOpen(true)}>Criar card desta sessão</button></div>
      )}
      {error && <span role="alert" style={{ color: 'var(--err)', fontSize: 13 }}>{error}</span>}
    </div>
  )
}

const pad = (n: number) => String(n).padStart(2, '0')
function eventText(e: DomainEvent): string {
  const d = (e.data ?? {}) as { path?: string; command?: string; text?: string }
  return d.path ?? d.command ?? d.text ?? ''
}

export function ControlRoom() {
  const { data, isPending, isError } = useOverview()
  const settings = useSettings().data
  const qc = useQueryClient()
  const [error, setError] = useState('')
  const pause = useMutation({
    mutationFn: (key: string) => patch(`/api/cards/${key}`, { column: 'pausado' }),
    onSuccess: () => { setError(''); qc.invalidateQueries({ queryKey: ['overview'] }); qc.invalidateQueries({ queryKey: ['cards'] }) },
    onError: (e: Error) => setError(e.message),
  })

  if (isError) return <p role="alert" style={{ color: 'var(--err)' }}>Não foi possível carregar a sala de controle.</p>
  if (isPending || !data) return <p className="placeholder">Carregando…</p>

  const { today, alerts, active, no_card, events } = data
  const agentsActive = [...new Set(active.map((a) => agentInfo(a.agent).name))].join(', ')
  const cardsActive = new Set(active.filter((a) => a.card).map((a) => a.card!.key)).size
  const sessionAgent = new Map<string, string>([...active, ...no_card].map((s) => [s.id, s.agent]))
  const sessionCard = new Map<string, string>(active.filter((a) => a.card).map((a) => [a.id, a.card!.key]))
  const cur = { currency: settings?.currency, rate: settings?.brl_rate }
  const dateText = new Date().toLocaleDateString('pt-BR', { weekday: 'long', day: 'numeric', month: 'long' })

  return (
    <div style={{ maxWidth: 1180, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 24 }}>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Sala de controle</h1>
        <p style={{ margin: 0, color: 'var(--text-2)' }}>
          {dateText} · {today.active_agents} {today.active_agents === 1 ? 'agente ativo' : 'agentes ativos'} em {cardsActive} {cardsActive === 1 ? 'card' : 'cards'} · atualizado ao vivo
        </p>
      </header>

      <section aria-label="Resumo de hoje" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <Metric label="Agentes ativos" value={today.active_agents} sub={agentsActive || 'nenhum agente rodando'} />
        <Metric
          label="Cards em andamento"
          value={today.cards_in_progress}
          sub={today.cards_without_live_session > 0 ? `${today.cards_without_live_session} sem sessão viva` : 'todos com sessão viva'}
          subColor={today.cards_without_live_session > 0 ? 'var(--warn)' : undefined}
        />
        <Metric
          label="Custo hoje"
          value={<Money usd={today.cost_usd} unpriced={today.cost_unpriced && today.cost_usd === 0} {...cur} />}
          sub={<>de <Money usd={today.daily_limit_usd} {...cur} /> somando os limites diários{today.cost_unpriced ? ' · há uso sem preço' : ''}</>}
        />
        <Metric
          label="Tokens hoje"
          value={formatTokens(today.tokens_input + today.tokens_output + today.tokens_cache_write + today.tokens_cache_read)}
          sub={`${formatTokens(today.tokens_input)} entrada · ${formatTokens(today.tokens_output)} saída · ${formatTokens(today.tokens_cache_write + today.tokens_cache_read)} cache`}
        />
      </section>

      <section aria-labelledby="alertas-t" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <h2 id="alertas-t" style={h2}>Precisa de atenção</h2>
        {error && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>{error}</p>}
        {alerts.length === 0 && <p style={{ margin: 0, color: 'var(--text-2)' }}>Nada precisa de atenção agora.</p>}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {alerts.map((a, i) => (
            <AlertRow key={`${a.kind}-${a.card_key}-${a.agent}-${i}`} alert={a} onPause={(k) => pause.mutate(k)} pausing={pause.isPending} />
          ))}
        </div>
      </section>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 20, alignItems: 'flex-start' }}>
        <section aria-labelledby="ativos-t" style={{ flex: '3 1 560px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <h2 id="ativos-t" style={h2}>Agentes rodando agora</h2>
          {active.length === 0 && <p style={{ margin: 0, color: 'var(--text-2)' }}>Nenhum agente rodando agora.</p>}
          {active.map((s) => <ActiveCard key={s.id} s={s} />)}
        </section>

        <div style={{ flex: '2 1 340px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <section aria-labelledby="avulsa-t" style={panel}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 8 }}>
              <h2 id="avulsa-t" style={h2}>Sessões sem card</h2>
              <span style={{ fontSize: 12, color: 'var(--text-2)' }}>contam no custo do dia</span>
            </div>
            {no_card.length === 0 && <span style={{ fontSize: 13, color: 'var(--text-2)' }}>Nenhuma sessão sem card.</span>}
            {no_card.map((s) => <NoCardRow key={s.id} s={s} />)}
          </section>

          <section aria-labelledby="limite-t" style={{ ...panel, gap: 14 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
              <h2 id="limite-t" style={h2}>Custo hoje × limite diário</h2>
              <Link to="/analytics" style={{ fontSize: 13 }}>Ver analytics</Link>
            </div>
            {today.per_agent.map((l) => {
              const warn = l.daily_usd > 0 && l.pct >= l.warn_pct
              const tip = l.daily_usd > 0
                ? `${agentInfo(l.agent).name}: US$ ${l.spent_usd.toFixed(2)} de US$ ${l.daily_usd.toFixed(2)} (${l.pct}%${warn ? ', acima do aviso' : ''})`
                : `${agentInfo(l.agent).name}: sem limite diário`
              return (
                <div key={l.agent} style={{ display: 'flex', flexDirection: 'column', gap: 6 }} title={tip}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, gap: 8 }}>
                    <AgentSwatch agent={l.agent} />
                    <span>
                      <strong style={{ fontWeight: 600 }}><Money usd={l.spent_usd} unpriced={l.unpriced && l.spent_usd === 0} {...cur} /></strong>{' '}
                      <span style={{ color: 'var(--text-2)' }}>
                        / {l.daily_usd > 0 ? <Money usd={l.daily_usd} {...cur} /> : 'sem limite'}
                        {l.daily_usd > 0 && ` · ${l.pct}%${warn ? ' · aviso' : ''}`}
                      </span>
                    </span>
                  </div>
                  <div role="img" aria-label={tip} style={{ position: 'relative', height: 8, borderRadius: 4, background: 'var(--border)' }}>
                    <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, borderRadius: 4, width: `${Math.min(l.pct, 100)}%`, background: agentInfo(l.agent).color, boxShadow: warn ? '0 0 0 2px #FAB219' : undefined }} />
                    <div aria-hidden="true" style={{ position: 'absolute', left: '80%', top: -3, width: 2, height: 14, background: '#6B7280' }} />
                  </div>
                </div>
              )
            })}
            <p style={{ margin: 0, fontSize: 12, color: 'var(--text-2)' }}>Marcador cinza = aviso em 80%. Em 100% a sessão é pausada.</p>
          </section>

          <section aria-labelledby="feed-t" style={panel}>
            <h2 id="feed-t" style={h2}>Eventos dos hooks</h2>
            {events.length === 0 && <span style={{ fontSize: 13, color: 'var(--text-2)' }}>Nenhum evento ainda.</span>}
            <ol style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 12 }}>
              {events.map((e) => {
                const at = new Date(e.at)
                const ag = sessionAgent.get(e.session_id)
                const ck = sessionCard.get(e.session_id)
                return (
                  <li key={e.id} style={{ display: 'grid', gridTemplateColumns: '46px 1fr', gap: 10, fontSize: 13 }}>
                    <span className="mono" style={{ color: 'var(--text-2)' }}>{pad(at.getHours())}:{pad(at.getMinutes())}</span>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                      <span style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--text-2)' }}>
                        {ag && <AgentSwatch agent={ag} />}{ag && ' · '}{ck && `${ck} · `}{e.kind}
                      </span>
                      <span className="mono" style={{ fontSize: 12.5, overflowWrap: 'anywhere' }}>{eventText(e)}</span>
                    </div>
                  </li>
                )
              })}
            </ol>
          </section>
        </div>
      </div>
    </div>
  )
}
