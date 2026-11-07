import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCards } from '../api/queries'
import { patch } from '../api/client'
import type { CardSummary, Column } from '../api/types'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Money } from '../ui/Money'
import { ReadinessPips } from '../ui/ReadinessPips'
import { NewTask } from './NewTask'
import { COLUMNS, groupByColumn, minutesSince, plural } from './board'

const FILTERS = [
  { id: 'todos', label: 'Todos' },
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'kiro', label: 'Kiro' },
]

const cardBox = (c: CardSummary, stale: boolean): React.CSSProperties => ({
  display: 'flex', flexDirection: 'column', gap: 8, padding: 14, borderRadius: 10, background: '#181B22',
  border: `1px solid ${stale ? '#4A3B17' : c.column === 'pausado' && !c.where_stopped ? '#4D2427' : '#262A33'}`,
  color: 'inherit',
})

function BoardCard({ card, onMove, busy }: { card: CardSummary; onMove: (key: string, col: Column) => void; busy: boolean }) {
  const stale = card.column === 'em_andamento' && !card.live
  const mins = minutesSince(card.last_activity_at)
  return (
    <article
      draggable
      onDragStart={(e) => { e.dataTransfer.setData('text/plain', card.key); e.dataTransfer.effectAllowed = 'move' }}
      style={cardBox(card, stale)}
      aria-label={`${card.key} ${card.title}`}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <span className="mono" style={{ fontSize: 12, color: 'var(--text-2)' }}>{card.key}</span>
        {card.live && (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5 }}>
            <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="3.5" fill="var(--ok)" /></svg>
            ao vivo
          </span>
        )}
        {stale && (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5, color: 'var(--warn)' }}>
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true"><path d="M12 3 2 21h20z" /><path d="M12 10v5M12 18v.5" /></svg>
            {card.last_activity_at ? `sem sessão há ${mins} min` : 'sem sessão viva'}
          </span>
        )}
      </div>
      <Link to={`/cards/${card.key}`} style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--text)', textDecoration: 'none', lineHeight: 1.35 }}>
        {card.title}
      </Link>
      {card.where_stopped && (
        <span style={{ fontSize: 12.5, color: 'var(--text-2)', lineHeight: 1.4 }}>Onde parei: {card.where_stopped}</span>
      )}
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '6px 12px', fontSize: 12, color: 'var(--text-2)', paddingTop: 6, borderTop: '1px solid var(--border)' }}>
        {card.agent && <span style={{ color: '#C3C8D2' }}><AgentSwatch agent={card.agent} /></span>}
        <span>{plural(card.session_count, 'sessão', 'sessões')}</span>
        <span style={{ color: 'var(--text)' }}><Money usd={card.totals.cost_usd} unpriced={card.totals.unpriced} /></span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-2)' }}>
        <ReadinessPips met={card.readiness.met} total={card.readiness.total} />
        <span>{card.readiness.ready ? 'pronto para handoff' : `${card.readiness.met}/${card.readiness.total} para handoff`}</span>
      </div>
      <label style={{ fontSize: 12, color: 'var(--text-2)', display: 'flex', alignItems: 'center', gap: 6 }}>
        Mover para…
        <select
          aria-label={`Mover ${card.key} para`}
          value={card.column}
          disabled={busy}
          onChange={(e) => onMove(card.key, e.target.value as Column)}
          style={{ minHeight: 32, background: 'var(--surface)', color: 'var(--text)', border: '1px solid #2E333E', borderRadius: 6, font: 'inherit', fontSize: 12 }}
        >
          {COLUMNS.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
        </select>
      </label>
    </article>
  )
}

export function Board() {
  const { data, isPending, isError } = useCards()
  const qc = useQueryClient()
  const [agent, setAgent] = useState('todos')
  const [error, setError] = useState('')
  const [over, setOver] = useState<Column | null>(null)

  const move = useMutation({
    mutationFn: ({ key, column }: { key: string; column: Column }) => patch(`/api/cards/${key}`, { column }),
    onSuccess: () => { setError(''); qc.invalidateQueries({ queryKey: ['cards'] }); qc.invalidateQueries({ queryKey: ['overview'] }) },
    // Sem atualização otimista: a coluna só muda quando o servidor aceita, então
    // falhar mantém o card onde estava; mostramos o erro do servidor.
    onError: (e: Error) => setError(e.message),
  })
  const onMove = (key: string, column: Column) => {
    const cur = data?.find((c) => c.key === key)
    if (!cur || cur.column === column) return
    setError('')
    move.mutate({ key, column })
  }

  const cards = (data ?? []).filter((c) => agent === 'todos' || c.agent === agent)
  const groups = groupByColumn(cards)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Board</h1>
          <p style={{ margin: 0, color: 'var(--text-2)' }}>As colunas seguem o estado real das sessões</p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'center' }}>
        <div role="group" aria-label="Filtrar por agente" style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
          {FILTERS.map((f) => {
            const on = agent === f.id
            return (
              <button
                key={f.id}
                type="button"
                aria-pressed={on}
                onClick={() => setAgent(f.id)}
                style={{
                  minHeight: 44, padding: '0 14px', borderRadius: 999, font: 'inherit', fontSize: 13, cursor: 'pointer',
                  background: on ? '#2A2F3A' : 'transparent', color: on ? '#fff' : '#C3C8D2',
                  border: `1px solid ${on ? '#4A505D' : '#2E333E'}`,
                }}
              >
                {f.label}
              </button>
            )
          })}
        </div>
        <NewTask />
        </div>
      </header>

      {error && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>{error}</p>}
      {isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os cards.</p>}
      {isPending && !isError && <p className="placeholder">Carregando…</p>}

      <div style={{ overflowX: 'auto', paddingBottom: 8 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, minmax(248px, 1fr))', gap: 14, alignItems: 'start', minWidth: 1290 }}>
          {COLUMNS.map((col) => (
            <section
              key={col.id}
              aria-label={col.label}
              onDragOver={(e) => { e.preventDefault(); setOver(col.id) }}
              onDragLeave={() => setOver((o) => (o === col.id ? null : o))}
              onDrop={(e) => {
                e.preventDefault()
                setOver(null)
                const key = e.dataTransfer.getData('text/plain')
                if (key) onMove(key, col.id)
              }}
              style={{
                display: 'flex', flexDirection: 'column', gap: 10, padding: 12, borderRadius: 12, background: '#121419',
                border: `1px solid ${over === col.id ? '#4A505D' : '#1F232B'}`, minHeight: 600,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '4px 4px 6px' }}>
                <h2 style={{ fontSize: 13, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span aria-hidden="true" style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: col.dot }} />
                  {col.label}
                </h2>
                <span className="mono" style={{ fontSize: 12, color: 'var(--text-2)' }}>{groups[col.id].length}</span>
              </div>
              <p style={{ margin: '-4px 4px 4px', fontSize: 12, color: '#7C8493' }}>{col.rule}</p>
              {groups[col.id].map((c) => <BoardCard key={c.key} card={c} onMove={onMove} busy={move.isPending} />)}
            </section>
          ))}
        </div>
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 24px', fontSize: 12, color: 'var(--text-2)' }}>
        <span>Bolinhas = critérios de “pronto para handoff” cumpridos (regra em Configuração).</span>
        <span>Arrastar um card é permitido; o daemon reconcilia com o estado real da sessão.</span>
      </div>
    </div>
  )
}
