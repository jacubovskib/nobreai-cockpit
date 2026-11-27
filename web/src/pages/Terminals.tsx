import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { isLive, useTerminals, type TerminalInfo } from '../api/terminals'
import { useCards, useProjects } from '../api/queries'
import type { CardSummary, Project } from '../api/types'
import { useProjectFilter } from '../app/project'
import { GridTile } from './terminals/GridTile'
import {
  FULL_H, chips, move, place, reconcile, sizeOf, step, tabs, toggleFull, visible,
  type Cols, type Layout, type Placed, type StateFilter, type TileSize,
} from './terminals/gridModel'
import { loadLayout, saveLayout } from './terminals/gridStore'
import { liveSet } from './terminals/liveBudget'
import { useOnScreen } from './terminals/useOnScreen'
import { usePageHidden } from './terminals/usePageHidden'

// Listas vazias fixas: um [] novo a cada render quebraria o memo dos tiles.
const NO_PROJECTS: Project[] = []
const NO_CARDS: CardSummary[] = []

type Action =
  | { type: 'reconcile'; newestFirst: string[] }
  | { type: 'reset'; newestFirst: string[] }
  | { type: 'cols'; cols: Cols }
  | { type: 'simple'; simple: boolean }
  | { type: 'tab'; tab: Layout['tab']; sub?: Layout['sub'] }
  | { type: 'sub'; sub: Layout['sub'] }
  | { type: 'step'; id: string; dir: 1 | -1 }
  | { type: 'full'; id: string }
  | { type: 'expand'; id: string }
  | { type: 'resize'; id: string; size: TileSize }
  | { type: 'move'; id: string; target: string; after: boolean }

const withSize = (l: Layout, id: string, s: TileSize): Layout => ({ ...l, sizes: { ...l.sizes, [id]: s } })
const sameList = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i])

function reducer(l: Layout, a: Action): Layout {
  switch (a.type) {
    case 'reconcile': {
      // Sem mudança devolve o mesmo objeto: a lista recarregada não regrava o layout.
      const n = reconcile(l, a.newestFirst, a.newestFirst)
      return sameList(n.order, l.order) && Object.keys(n.sizes).length === Object.keys(l.sizes).length ? l : n
    }
    case 'reset': return { ...l, order: a.newestFirst, sizes: {} }
    case 'cols': return { ...l, cols: a.cols }
    case 'simple': return { ...l, simple: a.simple }
    case 'tab': return { ...l, tab: a.tab, sub: a.sub ?? 'all' }
    case 'sub': return { ...l, sub: a.sub }
    case 'step': return withSize(l, a.id, step(sizeOf(l, a.id), a.dir, l.cols))
    case 'full': return withSize(l, a.id, toggleFull(sizeOf(l, a.id), l.cols))
    case 'expand': {
      // Linha toda; se já estiver, fica como está (toggleFull desfaria).
      const s = sizeOf(l, a.id)
      return s.span >= l.cols && s.h === FULL_H ? l : withSize(l, a.id, { span: l.cols, h: FULL_H })
    }
    case 'resize': return withSize(l, a.id, a.size)
    case 'move': {
      const order = move(l.order, a.id, a.target, a.after)
      return order === l.order ? l : { ...l, order }
    }
  }
}

// Quebras da grade: 2 colunas abaixo de 1100 px, 1 abaixo de 700 px. Só limita
// o que aparece; a preferência salva não muda.
const NARROW = '(max-width: 1099.98px)'
const PHONE = '(max-width: 699.98px)'
function readMaxCols(): Cols {
  if (typeof window.matchMedia !== 'function') return 3
  if (window.matchMedia(PHONE).matches) return 1
  return window.matchMedia(NARROW).matches ? 2 : 3
}
function useMaxCols(): Cols {
  const [max, setMax] = useState<Cols>(readMaxCols)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const lists = [window.matchMedia(NARROW), window.matchMedia(PHONE)]
    const on = () => setMax(readMaxCols())
    lists.forEach((m) => m.addEventListener?.('change', on))
    return () => lists.forEach((m) => m.removeEventListener?.('change', on))
  }, [])
  return max
}

// Ações dos tiles, todas por id. O objeto é criado uma vez só.
interface TileActions {
  focus(id: string): void
  wake(id: string): void
  step(id: string, dir: 1 | -1): void
  full(id: string): void
  resize(id: string, s: TileSize): void
  dragStart(id: string): void
  dragOver(id: string, after: boolean): void
  drop(id: string, after: boolean): void
  dragEnd(): void
}

interface SlotProps {
  t: TerminalInfo; project: Project | null; projects: Project[]
  span: number; h: number; cols: Cols; simple: boolean
  live: boolean; focused: boolean; dropMark: 'before' | 'after' | null
  actions: TileActions
}

// TileSlot recebe só valores simples e objetos estáveis, e monta o tamanho e
// os callbacks do GridTile por id: mexer num tile não redesenha os outros.
const TileSlot = memo(function TileSlot({ t, span, h, actions, ...rest }: SlotProps) {
  const id = t.id
  const size = useMemo(() => ({ span, h }), [span, h])
  const onFocus = useCallback(() => actions.focus(id), [actions, id])
  const onWake = useCallback(() => actions.wake(id), [actions, id])
  const onStep = useCallback((dir: 1 | -1) => actions.step(id, dir), [actions, id])
  const onToggleFull = useCallback(() => actions.full(id), [actions, id])
  const onResize = useCallback((s: TileSize) => actions.resize(id, s), [actions, id])
  const onDragStart = useCallback(() => actions.dragStart(id), [actions, id])
  const onDragOverTile = useCallback((after: boolean) => actions.dragOver(id, after), [actions, id])
  const onDropTile = useCallback((after: boolean) => actions.drop(id, after), [actions, id])
  return (
    <GridTile
      {...rest} t={t} size={size}
      onFocus={onFocus} onWake={onWake} onStep={onStep} onToggleFull={onToggleFull} onResize={onResize}
      onDragStart={onDragStart} onDragOverTile={onDragOverTile} onDropTile={onDropTile} onDragEnd={actions.dragEnd}
    />
  )
})

const seg = (on: boolean): React.CSSProperties => ({
  minHeight: 36, padding: '0 12px', borderRadius: 999, font: 'inherit', fontSize: 13, cursor: 'pointer',
  background: on ? '#2A2F3A' : 'transparent', color: on ? '#fff' : '#C3C8D2', border: `1px solid ${on ? '#4A505D' : '#2E333E'}`,
})
const groupStyle: React.CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: 6 }
const count: React.CSSProperties = { color: 'var(--text-2)' }
const COLS: Cols[] = [1, 2, 3]

export function Terminals() {
  const { data, isPending, isError } = useTerminals()
  const projectsQ = useProjects()
  const projects = projectsQ.data ?? NO_PROJECTS
  const cardsQ = useCards()
  const cards = Array.isArray(cardsQ.data) ? cardsQ.data : NO_CARDS
  const { root, app } = useProjectFilter()
  const [params] = useSearchParams()
  const selected = params.get('id') ?? ''
  const hasFilter = params.has('projeto')

  const [layout, dispatch] = useReducer(reducer, undefined, loadLayout)
  const [filter, setFilter] = useState<StateFilter>('all')
  const [focused, setFocused] = useState<string | null>(null)
  const [mark, setMark] = useState<{ id: string; after: boolean } | null>(null)
  // lastUsed e woken ficam em ref: lidos no render, mudam junto com o foco.
  const lastUsed = useRef<Record<string, number>>({})
  const woken = useRef(new Set<string>())
  const wasAlive = useRef(new Set<string>())
  const wasLive = useRef(new Set<string>())
  const dragging = useRef<string | null>(null)
  // Estado (não ref): a rolagem roda no render que já aplicou o "linha toda".
  const [scrollTo, setScrollTo] = useState<string | null>(null)
  const gridRef = useRef<HTMLDivElement>(null)
  const onScreen = useOnScreen(gridRef)
  const pageHidden = usePageHidden()
  const maxCols = useMaxCols()
  const cols = Math.min(layout.cols, maxCols) as Cols

  const actions = useMemo<TileActions>(() => ({
    focus(id) { lastUsed.current[id] = Date.now(); setFocused(id) },
    wake(id) { woken.current.add(id); lastUsed.current[id] = Date.now(); setFocused(id) },
    step(id, dir) { dispatch({ type: 'step', id, dir }) },
    full(id) { dispatch({ type: 'full', id }) },
    resize(id, size) { dispatch({ type: 'resize', id, size }) },
    dragStart(id) { dragging.current = id },
    dragOver(id, after) {
      if (dragging.current === null) return
      // Sobre o próprio tile não há onde soltar: tira o marcador.
      if (dragging.current === id) { setMark(null); return }
      setMark((m) => (m && m.id === id && m.after === after ? m : { id, after }))
    },
    drop(id, after) {
      const from = dragging.current
      dragging.current = null
      setMark(null)
      if (from !== null && from !== id) dispatch({ type: 'move', id: from, target: id, after })
    },
    dragEnd() { dragging.current = null; setMark(null) },
  }), [])

  // Mais novo primeiro, como a tabela de antes; terminal novo entra no início.
  const list = useMemo(() => [...(data ?? [])].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at)), [data])
  const placed = useMemo(() => place(list, projects, cards), [list, projects, cards])
  const placedById = useMemo(() => new Map<string, Placed>(placed.map((x) => [x.t.id, x])), [placed])
  const projectById = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects])
  const tabList = useMemo(() => tabs(placed, projects), [placed, projects])

  // Aba ou chip salvos sem terminal caem em "Todos" (sem regravar). Antes da
  // lista, dos projetos e dos cards chegarem, vale o salvo (o card decide o
  // projeto do terminal).
  const ready = !!data && !projectsQ.isPending && !cardsQ.isPending
  const tab = !ready || tabList.some((x) => x.id === layout.tab) ? layout.tab : 'all'
  const chipList = useMemo(() => chips(placed, projects, tab), [placed, projects, tab])
  const sub = !ready || layout.sub === 'all' || chipList.some((x) => x.id === layout.sub) ? layout.sub : 'all'
  const shown = visible(placed, { ...layout, tab, sub }, filter)

  // Terminal que encerra com o tile ligado continua ligado (entra em woken),
  // para quem assiste não perder a saída final. Tem de ser no render: um
  // efeito chegaria depois do render que já pausou o tile. Adicionar a um Set
  // é idempotente, então render repetido não muda nada.
  const ended = new Set(list.filter((t) => !isLive(t)).map((t) => t.id))
  for (const id of ended) if (wasAlive.current.has(id) && wasLive.current.has(id)) woken.current.add(id)

  const live = liveSet({
    ids: shown.map((t) => t.id),
    onScreen,
    ended,
    woken: woken.current,
    focused,
    lastUsed: lastUsed.current,
    pageHidden,
  })

  // Vivos e ligados do último render aplicado: a recarga seguinte compara com eles.
  useEffect(() => {
    wasAlive.current = new Set(list.filter(isLive).map((t) => t.id))
    wasLive.current = live
  })

  useEffect(() => {
    if (data) dispatch({ type: 'reconcile', newestFirst: list.map((t) => t.id) })
  }, [data, list])
  useEffect(() => { saveLayout(layout) }, [layout])

  // Abertura, uma vez: ?id= vence e abre "Todos"; sem id, o filtro global
  // escolhe a aba (depois que os projetos chegam); sem os dois, a última usada.
  const opened = useRef(false)
  useEffect(() => {
    if (opened.current) return
    if (selected) { opened.current = true; dispatch({ type: 'tab', tab: 'all' }); return }
    if (!hasFilter) { opened.current = true; return }
    if (projectsQ.isPending) return
    opened.current = true
    if (root) dispatch({ type: 'tab', tab: root.id, sub: app === null ? 'all' : app })
  }, [selected, hasFilter, projectsQ.isPending, root, app])

  // ?id=X, uma vez por id: em foco, linha toda, rolado até ele. Só conta como
  // atendido quando X está na lista: um terminal recém-criado pode chegar
  // numa recarga depois de uma lista velha do cache.
  const handledId = useRef('')
  useEffect(() => {
    if (!selected || !data || handledId.current === selected) return
    if (!data.some((t) => t.id === selected)) return
    handledId.current = selected
    dispatch({ type: 'expand', id: selected })
    actions.focus(selected)
    setScrollTo(selected)
  }, [selected, data, actions])
  // Sem dependências: rola assim que o tile aparecer na grade.
  useEffect(() => {
    if (!scrollTo) return
    const el = Array.from(gridRef.current?.querySelectorAll('[data-tile]') ?? []).find((x) => x.getAttribute('data-tile') === scrollTo)
    if (!el) return
    setScrollTo(null)
    el.scrollIntoView?.({ block: 'nearest' })
  })

  const current = list.find((t) => t.id === selected)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', marginRight: 'auto' }}>Terminais</h1>
        {list.length > 0 && (
          <>
            <div role="group" aria-label="Modo" style={groupStyle}>
              <button type="button" aria-pressed={layout.simple} style={seg(layout.simple)} onClick={() => dispatch({ type: 'simple', simple: true })}>Simples</button>
              <button type="button" aria-pressed={!layout.simple} style={seg(!layout.simple)} onClick={() => dispatch({ type: 'simple', simple: false })}>Detalhado</button>
            </div>
            <div role="group" aria-label="Colunas" style={groupStyle}>
              {COLS.map((n) => (
                <button key={n} type="button" aria-label={n === 1 ? '1 coluna' : `${n} colunas`} aria-pressed={layout.cols === n} style={seg(layout.cols === n)} onClick={() => dispatch({ type: 'cols', cols: n })}>{n}</button>
              ))}
            </div>
            <div role="group" aria-label="Estado" style={groupStyle}>
              <button type="button" aria-pressed={filter === 'all'} style={seg(filter === 'all')} onClick={() => setFilter('all')}>Todos</button>
              <button type="button" aria-pressed={filter === 'live'} style={seg(filter === 'live')} onClick={() => setFilter('live')}>Ao vivo</button>
            </div>
            <button type="button" style={seg(false)} onClick={() => dispatch({ type: 'reset', newestFirst: list.map((t) => t.id) })}>Restaurar layout</button>
          </>
        )}
      </div>
      {isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os terminais.</p>}
      {isPending && <p className="placeholder">Carregando…</p>}
      {!isPending && !isError && list.length === 0 && (
        <p style={{ margin: 0, color: 'var(--text-2)' }}>Nenhum terminal aberto. Inicie um agente em um card, em segundo plano, para vê-lo aqui.</p>
      )}
      {!isPending && !isError && selected && !current && (
        <p role="status" style={{ margin: 0, color: 'var(--text-2)' }}>Este terminal não existe mais (encerrado e removido).</p>
      )}
      {list.length > 0 && (
        <div role="tablist" aria-label="Projetos" style={groupStyle}>
          {tabList.map((x) => (
            <button key={x.id} type="button" role="tab" aria-selected={tab === x.id} style={seg(tab === x.id)} onClick={() => dispatch({ type: 'tab', tab: x.id })}>
              {x.name} <span style={count}>{x.count}</span>
            </button>
          ))}
        </div>
      )}
      {chipList.length > 0 && (
        <div role="group" aria-label="Subprojetos" style={groupStyle}>
          {chipList.map((x) => (
            <button key={x.id} type="button" aria-pressed={sub === x.id} style={seg(sub === x.id)} onClick={() => dispatch({ type: 'sub', sub: x.id })}>
              {x.name} <span style={count}>{x.count}</span>
            </button>
          ))}
        </div>
      )}
      {list.length > 0 && ready && shown.length === 0 && (
        <p style={{ margin: 0, color: 'var(--text-2)' }}>Nenhum terminal ao vivo nesta aba.</p>
      )}
      {/* A grade existe sempre: é a raiz que o useOnScreen observa. */}
      <div
        ref={gridRef}
        data-testid="grade"
        style={{ display: 'grid', gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, gap: 12, alignItems: 'start' }}
      >
        {shown.map((t) => {
          const x = placedById.get(t.id)
          const pid = x ? x.subId ?? x.rootId : null
          const s = sizeOf(layout, t.id)
          return (
            <TileSlot
              key={t.id}
              t={t}
              project={pid === null ? null : projectById.get(pid) ?? null}
              projects={projects}
              span={Math.min(s.span, cols)}
              h={s.h}
              cols={cols}
              simple={layout.simple}
              live={live.has(t.id)}
              focused={focused === t.id}
              dropMark={mark?.id === t.id ? (mark.after ? 'after' : 'before') : null}
              actions={actions}
            />
          )
        })}
      </div>
    </div>
  )
}
