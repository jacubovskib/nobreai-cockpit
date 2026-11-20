import type { CSSProperties } from 'react'
import { useSessionContext } from '../../api/queries'
import type { ContextLive } from '../../api/types'
import { contextLabel } from '../analytics/data'
import { muted } from '../card/shared'
import { barPct, formatTokensPt, pctText } from './format'

const OVER = 80
const chip: CSSProperties = { fontSize: 12, padding: '2px 8px', borderRadius: 999, border: '1px solid #4A3B17', background: '#1E1A12', color: 'var(--warn)' }

interface MeterProps {
  tokens: number
  windowTokens: number
  pct: number
  estimated?: boolean
  rewrite?: boolean
  idleOverTTL?: boolean
}

// Barra de uso da janela, com o texto "62% · 620 mil / 1 milhão" e os destaques.
export function ContextMeter({ tokens, windowTokens, pct, estimated, rewrite, idleOverTTL }: MeterProps) {
  const over = pct > OVER
  const text = `${pctText(pct)} · ${formatTokensPt(tokens)} / ${formatTokensPt(windowTokens)}`
  return (
    <div data-testid="context-meter" data-over={over ? 'true' : 'false'} style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, fontSize: 13 }}>
        <strong style={{ fontWeight: 500, color: over ? 'var(--err)' : 'var(--text)' }}>{text}</strong>
        {estimated && <span style={muted}>janela estimada</span>}
        {rewrite && <span style={chip}>regravação recente</span>}
        {idleOverTTL && <span style={chip}>parada além do cache</span>}
      </div>
      <div role="progressbar" aria-label="Contexto usado" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(barPct(pct))} style={{ height: 8, borderRadius: 4, background: 'var(--border)', overflow: 'hidden' }}>
        <div data-testid="context-bar" style={{ height: '100%', width: `${barPct(pct)}%`, background: over ? 'var(--err)' : 'var(--ok)' }} />
      </div>
    </div>
  )
}

export function MeterOf({ c }: { c: ContextLive }) {
  return <ContextMeter tokens={c.tokens} windowTokens={c.window_tokens} pct={c.pct} estimated={c.window_estimated} rewrite={!!c.recent_rewrite} idleOverTTL={c.idle_over_ttl} />
}

const minutes = (sec: number) => (sec < 60 ? 'menos de 1 min' : `${Math.round(sec / 60)} min`)

// Detalhe do medidor: crescimento, turnos até o limite, compactações, cache, parada e maiores saídas.
export function ContextInfo({ c }: { c: ContextLive }) {
  const rows: [string, string][] = [
    ['Crescimento por turno', c.growth_per_turn > 0 ? `${formatTokensPt(c.growth_per_turn)} tokens` : 'ainda sem dados'],
    ['Turnos até o limite', c.at_limit ? 'no limite' : c.turns_to_limit > 0 ? String(c.turns_to_limit) : 'sem crescimento'],
    ['Compactações', String(c.compactions)],
    ['Turnos desde a última compactação', String(c.turns_since_compact)],
    ['Cache aproveitado', `${Math.round(c.cache_hit_pct)}%`],
    ['Parada', `${minutes(c.idle_sec)} (cache de ${c.ttl === '1h' ? '1 h' : '5 min'})${c.idle_over_ttl ? ' · além do cache' : ''}`],
  ]
  if (c.recent_rewrite) rows.push(['Regravação recente', `${formatTokensPt(c.recent_rewrite.tokens)} tokens`])
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <dl style={{ margin: 0, display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))', gap: '8px 20px', fontSize: 13 }}>
        {rows.map(([k, v]) => (
          <div key={k}><dt style={muted}>{k}</dt><dd style={{ margin: 0 }}>{v}</dd></div>
        ))}
      </dl>
      <div>
        <span style={muted}>Maiores saídas na janela</span>
        {c.top_outputs.length === 0 ? (
          <p style={{ ...muted, margin: '4px 0 0' }}>Nenhuma saída grande.</p>
        ) : (
          <ol style={{ margin: '4px 0 0', padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
            {c.top_outputs.slice(0, 5).map((o, i) => (
              <li key={i} style={{ display: 'flex', gap: 10, justifyContent: 'space-between' }}>
                <span className="mono" style={{ overflowWrap: 'anywhere' }}>{contextLabel(o.tool, o.key)}</span>
                <span style={{ ...muted, whiteSpace: 'nowrap' }}>{formatTokensPt(o.tokens)} tokens</span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  )
}

// Carrega o medidor da sessão; `expanded` abre o detalhe. Usado na sala de controle e no card.
export function SessionContext({ id, summary, expanded }: { id: string; summary?: { tokens: number; window_tokens: number; pct: number; rewrite: boolean; idle_over_ttl: boolean }; expanded: boolean }) {
  const q = useSessionContext(id)
  const c = q.data
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {c ? <MeterOf c={c} /> : summary ? <ContextMeter tokens={summary.tokens} windowTokens={summary.window_tokens} pct={summary.pct} rewrite={summary.rewrite} idleOverTTL={summary.idle_over_ttl} /> : <span style={muted}>Carregando contexto…</span>}
      {expanded && c && <ContextInfo c={c} />}
      {expanded && q.isError && <span role="alert" style={{ ...muted, color: 'var(--err)' }}>Não foi possível ler o contexto.</span>}
    </div>
  )
}
