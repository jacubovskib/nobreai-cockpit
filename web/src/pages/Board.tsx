import { useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCards, useProjects } from '../api/queries'
import { patch } from '../api/client'
import type { CardSummary, CardType, Column } from '../api/types'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Credits, Money } from '../ui/Money'
import { ReadinessPips } from '../ui/ReadinessPips'
import { ProjectTag } from '../ui/ProjectTag'
import { useProjectFilter, useWithProject } from '../app/project'
import { NewTask } from './NewTask'
import { COLUMNS, DONE_VISIBLE, PRIORITY_LABEL, TYPE_ICON, TYPE_LABEL, ageText, applyBoardFilter, boardStrip, cardMarks, epicOptions, groupByColumn, handoffHint, hasSession, isBareBacklog, minutesSince, plural, recentDone, signalColor, signalText } from './boardModel'

const TYPES = Object.keys(TYPE_LABEL) as CardType[]

const FILTERS = [
  { id: 'todos', label: 'Todos' },
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'kiro', label: 'Kiro' },
]

const selectStyle: React.CSSProperties = { minHeight: 44, background: 'var(--surface)', color: 'var(--text)', border: '1px solid #2E333E', borderRadius: 6, font: 'inherit', fontSize: 13 }

const nowrap: CSSProperties = { whiteSpace: 'nowrap', flex: 'none' }
// "Onde parei" no board: no máximo 2 linhas; o texto completo fica na página do card.
const clamp2: CSSProperties = { display: '-webkit-box', WebkitBoxOrient: 'vertical', WebkitLineClamp: 2, overflow: 'hidden', fontSize: 12.5, color: 'var(--text-2)', lineHeight: 1.4 }
const action: CSSProperties = { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', minHeight: 40, padding: '0 14px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', fontSize: 13, fontWeight: 500, textDecoration: 'none' }
const actionPrimary: CSSProperties = { ...action, border: '1px solid #E8EAEE', background: '#E8EAEE', color: '#0E1014' }
const smallBtn: CSSProperties = { minHeight: 32, padding: '0 10px', borderRadius: 6, border: '1px solid #2E333E', background: 'var(--surface)', color: 'var(--text)', font: 'inherit', fontSize: 12, cursor: 'pointer' }
const stripBtn: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 6, minHeight: 44, padding: '0 14px', borderRadius: 10, border: '1px solid #2E333E', background: '#181B22', color: 'var(--text)', font: 'inherit', fontSize: 13, cursor: 'pointer' }
const linkBtn: CSSProperties = { alignSelf: 'flex-start', minHeight: 32, padding: '0 4px', background: 'none', border: 'none', color: 'var(--text)', textDecoration: 'underline', font: 'inherit', fontSize: 13, cursor: 'pointer' }

const colId = (c: Column) => `board-col-${c}`

const cardBox = (c: CardSummary, stale: boolean): React.CSSProperties => ({
  display: 'flex', flexDirection: 'column', gap: 8, padding: 14, borderRadius: 10, background: '#181B22',
  border: `1px solid ${stale ? '#4A3B17' : c.column === 'pausado' && !c.where_stopped ? '#4D2427' : '#262A33'}`,
  color: 'inherit',
})

function BoardCard({ card, onMove, busy, tag }: { card: CardSummary; onMove: (key: string, col: Column) => void; busy: boolean; tag?: React.ReactNode }) {
  const withP = useWithProject()
  const signal = signalText(card)
  const isEpic = card.type === 'epico'
  // Épico em andamento não tem sessão por desenho: nunca mostra o aviso de sessão parada.
  const stale = card.column === 'em_andamento' && !card.live && !signal && !isEpic
  const totals = isEpic && card.tree_totals ? card.tree_totals : card.totals
  const labels = card.labels ?? []
  const hot = card.priority === 'alta' || card.priority === 'mais_alta'
  const mins = minutesSince(card.last_activity_at)
  const staleText = card.pid_dead_at
    ? `processo encerrado há ${minutesSince(card.pid_dead_at)} min`
    : card.last_activity_at ? `sem sessão há ${mins} min` : 'sem sessão viva'
  const [moving, setMoving] = useState(false)
  const moveBtn = useRef<HTMLButtonElement>(null)
  // Fechar as opções devolve o foco ao botão ⋯ (senão ele cairia no body).
  const closeMove = () => { setMoving(false); moveBtn.current?.focus() }
  const cardTo = withP(`/cards/${card.key}`)
  // Backlog sem sessão: só chave, tipo e título (e o que você definiu), sem rodapé zerado.
  const bare = isBareBacklog(card)
  const hint = handoffHint(card)
  const marks = cardMarks(card, card.readiness.ready)
  const merged = !!card.branch_merged_at && card.column !== 'concluido'
  const showFooter = !bare && (isEpic || hasSession(card) || !!card.agent)
  return (
    <article
      draggable
      onDragStart={(e) => { e.dataTransfer.setData('text/plain', card.key); e.dataTransfer.effectAllowed = 'move' }}
      onKeyDown={(e) => { if (e.key === 'Escape' && moving) closeMove() }}
      style={cardBox(card, stale)}
      aria-label={`${card.key} ${card.title}`}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 8px' }}>
        <span className="mono" data-testid="card-key" style={{ ...nowrap, fontSize: 12, color: 'var(--text-2)' }}>{card.key}</span>
        {card.type && card.type !== 'tarefa' && (
          <span style={{ ...nowrap, fontSize: 11.5, color: 'var(--text-2)' }}><span aria-hidden="true">{TYPE_ICON[card.type]}</span> {TYPE_LABEL[card.type]}</span>
        )}
        <span style={{ marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          {card.live && (
            <span style={{ ...nowrap, display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5 }}>
              <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="3.5" fill="var(--ok)" /></svg>
              ao vivo
            </span>
          )}
          {hint && (
            <span className="badge" style={{ ...nowrap, display: 'inline-flex', alignItems: 'center', gap: 6, color: card.readiness.ready ? 'var(--ok)' : undefined }}>
              {!card.readiness.ready && <ReadinessPips met={card.readiness.met} total={card.readiness.total} />}
              {hint}
            </span>
          )}
          <button
            ref={moveBtn}
            type="button"
            aria-label={`Mover ${card.key}`}
            aria-expanded={moving}
            title="Mover para outra coluna"
            onClick={() => setMoving((v) => !v)}
            style={{ ...smallBtn, minWidth: 32, padding: 0, background: moving ? '#2A2F3A' : 'transparent', color: 'var(--text-2)', fontSize: 16, lineHeight: 1 }}
          >
            <span aria-hidden="true">⋯</span>
          </button>
        </span>
      </div>
      {moving && (
        <div role="group" aria-label={`Mover ${card.key} para`} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-2)' }}>
          <span>Mover para</span>
          {COLUMNS.filter((c) => c.id !== card.column).map((c) => (
            <button key={c.id} type="button" disabled={busy} style={smallBtn} onClick={() => { closeMove(); onMove(card.key, c.id) }}>
              {c.label}
            </button>
          ))}
        </div>
      )}
      {(signal || card.waiting_on_you || marks.length > 0 || merged || stale) && (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 8px' }}>
          {signal && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5, color: signalColor(card.signal) }}>
              {card.signal === 'parado'
                ? <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true"><path d="M12 3 2 21h20z" /><path d="M12 10v5M12 18v.5" /></svg>
                : <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></svg>}
              {signal}
            </span>
          )}
          {card.waiting_on_you && <span className="badge">{card.waiting_reason ? `esperando você · ${card.waiting_reason}` : 'esperando você'}</span>}
          {marks.map((m) => <span key={m} className="badge">{m}</span>)}
          {merged && <span className="badge">branch integrada</span>}
          {stale && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5, color: 'var(--warn)' }}>
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true"><path d="M12 3 2 21h20z" /><path d="M12 10v5M12 18v.5" /></svg>
              {staleText}
            </span>
          )}
        </div>
      )}
      <Link to={cardTo} style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--text)', textDecoration: 'none', lineHeight: 1.35 }}>
        {card.title}
      </Link>
      {tag}
      {card.parent_key && (
        <Link to={withP(`/cards/${card.parent_key}`)} style={{ fontSize: 12, color: 'var(--text-2)', textDecoration: 'none' }}>
          ◆ {card.parent_title}
        </Link>
      )}
      {(card.points != null || (card.priority && card.priority !== 'media') || labels.length > 0) && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 10px', fontSize: 12, color: 'var(--text-2)' }}>
          {card.points != null && <span>{card.points.toLocaleString('pt-BR')} pts</span>}
          {card.priority && card.priority !== 'media' && (
            <span style={{ color: hot ? 'var(--warn)' : 'var(--text-2)' }}>{PRIORITY_LABEL[card.priority]}</span>
          )}
          {labels.map((l) => <span key={l} className="badge">{l}</span>)}
        </div>
      )}
      {card.where_stopped && (
        <span title={card.where_stopped} style={clamp2}>Onde parei: {card.where_stopped}</span>
      )}
      {showFooter && (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '6px 12px', fontSize: 12, color: 'var(--text-2)', paddingTop: 6, borderTop: '1px solid var(--border)' }}>
          {card.agent && <span style={{ color: '#C3C8D2' }}><AgentSwatch agent={card.agent} /></span>}
          {card.session_count > 0 && <span>{plural(card.session_count, 'sessão', 'sessões')}</span>}
          <span style={{ color: 'var(--text)' }}><Money usd={totals.cost_usd} unpriced={totals.unpriced} /><Credits credits={totals.credits} /></span>
        </div>
      )}
      {isEpic && card.children && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-2)' }}>
          <progress aria-label="Filhas concluídas" max={Math.max(card.children.total, 1)} value={card.children.done} style={{ flex: 1, height: 6 }} />
          <span className="mono">{card.children.done}/{card.children.total}</span>
        </div>
      )}
      {/* Continuar pede agente, prompt e modo: leva ao painel "Continuar" da página do card.
          Revisar e aprovar leva à página do card, onde fica "Aprovar e concluir". */}
      {card.column === 'pausado' && !isEpic && (
        <Link to={`${cardTo}#cont-t`} aria-label={`Continuar ${card.key}`} style={action}>Continuar</Link>
      )}
      {card.column === 'revisao' && (
        <Link to={cardTo} aria-label={`Revisar e aprovar ${card.key}`} style={actionPrimary}>Revisar e aprovar</Link>
      )}
    </article>
  )
}

export function Board() {
  const { data, isPending, isError } = useCards()
  const qc = useQueryClient()
  const { ids } = useProjectFilter()
  const projects = useProjects().data ?? []
  const [agent, setAgent] = useState('todos')
  const [search, setSearch] = useState('')
  const [doneOpen, setDoneOpen] = useState(false)
  const [params, setParams] = useSearchParams()
  const epicParam = params.get('epico') ?? ''
  const typeParam = params.get('tipo') ?? ''
  const filter = {
    epic: epicParam,
    type: (TYPES as string[]).includes(typeParam) ? (typeParam as CardType) : '',
    waiting: params.get('espera') === '1',
  } as const
  const setParam = (k: string, v: string) => setParams((prev) => {
    const q = new URLSearchParams(prev)
    if (v) q.set(k, v); else q.delete(k)
    return q
  }, { replace: true })
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

  const all = data ?? []
  const inProject = (c: CardSummary) => ids === null || (c.project_id !== null && ids.includes(c.project_id))
  const byProject = all.filter(inProject)
  // Épico de um grupo-pai fica visível quando filtramos um sub-projeto: com filtro de épico
  // ativo ou quando alguma filha visível aponta para ele.
  const parentIds = new Set(ids === null ? [] : projects.filter((p) => ids.includes(p.id) && p.parent_id != null).map((p) => p.parent_id as number))
  const visibleParents = new Set(byProject.map((c) => c.parent_key).filter(Boolean))
  const groupEpics = all.filter((c) => c.type === 'epico' && !inProject(c) && parentIds.has(c.project_id)
    && (c.key === filter.epic || visibleParents.has(c.key)))
  const scoped = [...groupEpics, ...byProject]
  const options = epicOptions(scoped, filter.epic)
  const withoutWaiting = applyBoardFilter(scoped.filter((c) => c.type === 'epico' || agent === 'todos' || c.agent === agent), { ...filter, waiting: false, search })
  const waitingCount = withoutWaiting.filter((c) => c.waiting_on_you).length
  const cards = filter.waiting ? withoutWaiting.filter((c) => c.waiting_on_you) : withoutWaiting
  const groups = groupByColumn(cards)
  // A faixa conta sobre os mesmos cards que as colunas mostram.
  const strip = boardStrip(cards)
  const oldest = ageText(strip.oldestReview)
  const goTo = (col: Column) => {
    const el = document.getElementById(colId(col))
    el?.scrollIntoView?.({ behavior: 'smooth', block: 'nearest', inline: 'start' })
    el?.focus?.({ preventScroll: true })
  }
  const num: CSSProperties = { fontWeight: 600, fontVariantNumeric: 'tabular-nums' }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Board</h1>
          <p style={{ margin: 0, color: 'var(--text-2)' }}>As colunas seguem o estado real das sessões</p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'center' }}>
        <input
          type="search"
          aria-label="Buscar card"
          placeholder="Buscar por chave ou título"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ ...selectStyle, width: 240, padding: '0 12px' }}
        />
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
        <label style={{ fontSize: 13, color: 'var(--text-2)', display: 'flex', alignItems: 'center', gap: 6 }}>
          Épico
          <select aria-label="Filtrar por épico" value={filter.epic} onChange={(e) => setParam('epico', e.target.value)} style={selectStyle}>
            <option value="">Todos</option>
            <option value="none">Sem épico</option>
            {options.map((o) => <option key={o.key} value={o.key}>{o.title ? `${o.key} ${o.title}` : o.key}</option>)}
          </select>
        </label>
        <label style={{ fontSize: 13, color: 'var(--text-2)', display: 'flex', alignItems: 'center', gap: 6 }}>
          Tipo
          <select aria-label="Filtrar por tipo" value={filter.type} onChange={(e) => setParam('tipo', e.target.value)} style={selectStyle}>
            <option value="">Todos</option>
            {TYPES.map((t) => <option key={t} value={t}>{TYPE_LABEL[t]}</option>)}
          </select>
        </label>
        <button
          type="button"
          aria-pressed={filter.waiting}
          onClick={() => setParam('espera', filter.waiting ? '' : '1')}
          style={{
            minHeight: 44, padding: '0 14px', borderRadius: 999, font: 'inherit', fontSize: 13, cursor: 'pointer',
            background: filter.waiting ? '#2A2F3A' : 'transparent', color: filter.waiting ? '#fff' : '#C3C8D2',
            border: `1px solid ${filter.waiting ? '#4A505D' : '#2E333E'}`,
          }}
        >
          Esperando você · {waitingCount}
        </button>
        <NewTask />
        </div>
      </header>

      {error && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>{error}</p>}
      {isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os cards.</p>}
      {isPending && !isError && <p className="placeholder">Carregando…</p>}

      {(strip.review > 0 || strip.resumable > 0 || strip.live > 0) && (
        <div role="group" aria-label="O que espera por você" style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          {strip.review > 0 && (
            <button type="button" style={{ ...stripBtn, borderColor: '#3B3364' }} onClick={() => goTo('revisao')}>
              <span style={num}>{strip.review}</span> aguardando sua revisão
              {oldest && <span style={{ color: 'var(--text-2)' }}>· a mais antiga {oldest}</span>}
            </button>
          )}
          {strip.resumable > 0 && (
            <button type="button" style={stripBtn} onClick={() => goTo('pausado')}>
              <span style={num}>{strip.resumable}</span> {strip.resumable === 1 ? 'pausado pronto para retomar' : 'pausados prontos para retomar'}
            </button>
          )}
          {strip.live > 0 && (
            <button type="button" style={stripBtn} onClick={() => goTo('em_andamento')}>
              <span style={num}>{strip.live}</span> {strip.live === 1 ? 'agente ao vivo' : 'agentes ao vivo'}
            </button>
          )}
        </div>
      )}

      <div style={{ overflowX: 'auto', paddingBottom: 8 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, minmax(248px, 1fr))', gap: 14, alignItems: 'start', minWidth: 1290 }}>
          {COLUMNS.map((col) => {
            const total = groups[col.id].length
            const shown = col.id === 'concluido' ? recentDone(groups[col.id], doneOpen ? Infinity : DONE_VISIBLE) : groups[col.id]
            return (
            <section
              key={col.id}
              id={colId(col.id)}
              tabIndex={-1}
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
                <h2 title={col.rule} style={{ fontSize: 13, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span aria-hidden="true" style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: col.dot }} />
                  {col.label}
                </h2>
                <span className="mono" style={{ fontSize: 12, color: 'var(--text-2)' }}>{total}</span>
              </div>
              {shown.map((c) => <BoardCard key={c.key} card={c} onMove={onMove} busy={move.isPending} tag={ids === null || ids.length > 1 ? <ProjectTag project={projects.find((p) => p.id === c.project_id) ?? null} projects={projects} /> : undefined} />)}
              {col.id === 'concluido' && total > DONE_VISIBLE && (
                <button type="button" aria-expanded={doneOpen} style={linkBtn} onClick={() => setDoneOpen((v) => !v)}>
                  {doneOpen ? `Mostrar só os ${DONE_VISIBLE} mais recentes` : `Ver os ${total} concluídos`}
                </button>
              )}
            </section>
            )
          })}
        </div>
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 24px', fontSize: 12, color: 'var(--text-2)' }}>
        <span>“contexto n/5” = critérios de “pronto para handoff” cumpridos (regra em Configuração); passe o mouse no nome da coluna para ver a regra dela.</span>
        <span>Arrastar um card é permitido; “Em andamento” só aceita card com sessão viva — use Continuar com… ou nobre resume.</span>
      </div>
    </div>
  )
}
