import { memo, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { isLive, type TerminalInfo } from '../../api/terminals'
import type { Project } from '../../api/types'
import { useWithProject } from '../../app/project'
import { ProjectTag } from '../../ui/ProjectTag'
import { agentInfo } from '../../ui/agents'
import { LazyTerminalView } from '../../ui/LazyTerminalView'
import type { Status, TerminalHandle } from '../../ui/TerminalView'
import { minutesSince } from '../boardModel'
import { clampH, snapSpan, type Cols, type TileSize } from './gridModel'
import { CTX_DEFAULT_LIMIT, ctxLevel } from './contextLevel'
import { CtxChip, CtxStrip, CtxWarning } from './TileContext'

export interface GridTileProps {
  t: TerminalInfo; project: Project | null; projects: Project[]
  size: TileSize; cols: Cols; simple: boolean
  live: boolean            // ligado pelo orçamento
  focused: boolean
  onFocus(): void          // clique ou foco dentro do tile
  onWake(): void           // clique no corpo em pausa
  onStep(dir: 1 | -1): void; onToggleFull(): void
  onResize(s: TileSize): void          // ao soltar o canto
  onDragStart(): void; onDragOverTile(after: boolean): void; onDropTile(after: boolean): void; onDragEnd(): void
  dropMark: 'before' | 'after' | null
  cardKey?: string         // card efetivo do tile; ausente = t.card_key
  sessionId?: string       // '' ou ausente = sem sessão casada
  ctxPct?: number | null   // inteiro; null ou ausente = sem dado
  ctxTokens?: number; ctxWindow?: number; ctxPctOnly?: boolean
  ctxLimit?: number        // ausente = 80
}

const btn: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', minWidth: 28, minHeight: 28, padding: '0 8px', borderRadius: 6, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer' }
const btnDanger: React.CSSProperties = { ...btn, borderColor: '#6b2c2c', color: 'var(--err)' }
const menuItem: React.CSSProperties = { ...btn, justifyContent: 'flex-start', border: 0, width: '100%', minHeight: 34 }

// Estado do gesto de redimensionar: medidas tomadas ao apertar o canto.
interface Gesture { x: number; y: number; w: number; h: number; colW: number; gap: number; last: TileSize }

function GridTileView(p: GridTileProps) {
  const { t, project, projects, size, cols, simple, live, focused } = p
  const withP = useWithProject()
  const root = useRef<HTMLElement>(null)
  const term = useRef<TerminalHandle>(null)
  const gesture = useRef<Gesture | null>(null)
  const [ro, setRo] = useState(true)
  const [status, setStatus] = useState<Status>('conectando')
  const [endedHere, setEndedHere] = useState(false)
  const [menu, setMenu] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [hint, setHint] = useState('')

  const ended = !isLive(t) || endedHere
  const agent = agentInfo(t.agent)
  const card = p.cardKey ?? t.card_key
  const limit = p.ctxLimit ?? CTX_DEFAULT_LIMIT
  const pct = p.ctxPct ?? null
  const showCtx = !ended && pct !== null
  const canRestart = !ended && !!p.sessionId && card !== ''
  const openRestart = () => {} // painel na Task 7

  // Em pausa o terminal é desmontado: ao ligar de novo abre assistindo.
  useEffect(() => {
    if (live) return
    setRo(true); setStatus('conectando'); setMenu(false); setConfirming(false)
  }, [live])
  useEffect(() => { if (!live || ended) { setMenu(false); setConfirming(false) } }, [live, ended])

  // Posição do ponteiro na metade direita do tile = soltar depois dele.
  const isAfter = (clientX: number) => {
    const r = root.current?.getBoundingClientRect()
    return !!r && clientX > r.left + r.width / 2
  }

  const apply = (s: TileSize) => {
    const el = root.current
    if (!el) return
    el.style.height = `${s.h}px`
    el.style.gridColumn = `span ${s.span}`
  }

  const onCornerDown = (e: React.PointerEvent<HTMLButtonElement>) => {
    const el = root.current
    if (!el) return
    e.preventDefault()
    try { e.currentTarget.setPointerCapture?.(e.pointerId) } catch { /* ponteiro sintético */ }
    const rect = el.getBoundingClientRect()
    const grid = el.parentElement
    const gap = grid ? parseFloat(getComputedStyle(grid).columnGap) || 0 : 0
    const colW = grid ? (grid.clientWidth - gap * (cols - 1)) / cols : 0
    gesture.current = { x: e.clientX, y: e.clientY, w: rect.width, h: rect.height || size.h, colW, gap, last: size }
    // Durante o gesto só o CSS muda; o xterm é ajustado uma vez, ao soltar.
    // Se o tile ou o terminal desmontar no meio, a trava some com a view.
    term.current?.holdFit()
  }
  const onCornerMove = (e: React.PointerEvent<HTMLButtonElement>) => {
    const g = gesture.current
    if (!g) return
    // Sem medida de coluna (contêiner sem tamanho), a largura não muda.
    const span = g.colW > 0 ? snapSpan(g.w + e.clientX - g.x, g.colW, g.gap, cols) : size.span
    const next = { span, h: clampH(g.h + e.clientY - g.y) }
    g.last = next
    apply(next)
    setHint(`${next.span} de ${cols} col · ${next.h} px`)
  }
  const onCornerUp = (cancel: boolean) => (e: React.PointerEvent<HTMLButtonElement>) => {
    const g = gesture.current
    if (!g) return
    gesture.current = null
    try { e.currentTarget.releasePointerCapture?.(e.pointerId) } catch { /* já solto */ }
    setHint('')
    if (cancel) { apply(size); term.current?.releaseFit(); return }
    p.onResize(g.last)
    term.current?.releaseFit()
  }

  const stateText = ended
    ? `encerrado${t.exit_code != null ? ` (código ${t.exit_code})` : ''}`
    : null
  const lost = live && !ended && status === 'desconectado'

  const dropShadow = p.dropMark === 'before' ? 'inset 4px 0 0 var(--accent, #6ea8ff)' : p.dropMark === 'after' ? 'inset -4px 0 0 var(--accent, #6ea8ff)' : undefined

  return (
    <section
      ref={root}
      data-tile={t.id}
      aria-label={`Terminal ${agent.name}${card ? ` do card ${card}` : ''}`}
      onClick={p.onFocus}
      onFocus={p.onFocus}
      onDragOver={(e) => { e.preventDefault(); p.onDragOverTile(isAfter(e.clientX)) }}
      onDrop={(e) => { e.preventDefault(); p.onDropTile(isAfter(e.clientX)) }}
      style={{
        gridColumn: `span ${size.span}`, height: size.h, contain: 'layout paint', position: 'relative',
        display: 'flex', flexDirection: 'column', minWidth: 0, borderRadius: 10, background: 'var(--surface, #14171D)',
        border: `1px solid ${focused ? 'var(--accent, #6ea8ff)' : 'var(--border)'}`, boxShadow: dropShadow,
      }}
    >
      <header
        data-testid="tile-header"
        draggable
        onDragStart={(e) => {
          e.dataTransfer?.setData?.('text/plain', t.id)
          if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
          p.onDragStart()
        }}
        onDragEnd={p.onDragEnd}
        style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, padding: '6px 8px', borderBottom: '1px solid var(--border)', fontSize: 13 }}
      >
        <span aria-hidden="true" title="Arrastar para reordenar" style={{ cursor: 'grab', color: 'var(--text-2)' }}>⠿</span>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontWeight: 600 }}>
          <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="4" fill={agent.color} /></svg>
          {agent.name}
        </span>
        {card
          ? <Link to={withP(`/cards/${card}`)} className="mono">{card}</Link>
          : <span style={{ color: 'var(--text-2)' }}>sem card</span>}
        {showCtx && simple && <CtxChip pct={pct} limit={limit} />}
        {ended
          ? <span role="status" style={{ color: 'var(--text-2)' }}>{stateText}</span>
          : lost
            ? <span role="status" style={{ color: 'var(--warn)' }}>Desconectado</span>
            : <span role="status" style={{ color: 'var(--ok)' }}>{live && status !== 'conectado' ? status : 'ao vivo'}</span>}
        {lost && <button type="button" style={btn} onClick={() => term.current?.reconnect()}>Tentar de novo</button>}
        {!simple && (
          <>
            <ProjectTag project={project} projects={projects} />
            <span style={{ color: 'var(--text-2)' }}>{t.origin}</span>
            <span style={{ color: 'var(--text-2)' }}>há {minutesSince(t.started_at)} min</span>
            <span style={{ color: 'var(--text-2)' }}>{t.clients} {t.clients === 1 ? 'cliente' : 'clientes'}</span>
          </>
        )}
        <span style={{ marginLeft: 'auto', display: 'inline-flex', gap: 4, position: 'relative' }}>
          <button type="button" style={btn} aria-label="Diminuir terminal" onClick={() => p.onStep(-1)}>−</button>
          <button type="button" style={btn} aria-label="Aumentar terminal" onClick={() => p.onStep(1)}>+</button>
          <button type="button" style={btn} aria-label="Ocupar a linha toda" onClick={p.onToggleFull}>⤢</button>
          {live && !ended && (
            <button type="button" style={btn} aria-label="Mais ações" aria-haspopup="menu" aria-expanded={menu} onClick={() => setMenu((m) => !m)}>⋯</button>
          )}
          {menu && live && !ended && (
            <div
              role="menu"
              aria-label="Ações do terminal"
              onKeyDown={(e) => { if (e.key === 'Escape') setMenu(false) }}
              style={{ position: 'absolute', top: '100%', right: 0, zIndex: 5, minWidth: 170, padding: 4, borderRadius: 8, border: '1px solid #3A3F4B', background: 'var(--surface, #14171D)' }}
            >
              <button type="button" role="menuitem" style={menuItem} onClick={() => { setMenu(false); term.current?.setReadonly(!ro) }}>
                {ro ? 'Interagir' : 'Somente assistir'}
              </button>
              <button type="button" role="menuitem" style={{ ...menuItem, color: 'var(--err)' }} onClick={() => { setMenu(false); setConfirming(true) }}>Encerrar agente</button>
            </div>
          )}
        </span>
      </header>
      {showCtx && canRestart && ctxLevel(pct, limit) !== 'ok' && (
        <CtxWarning pct={pct} limit={limit} simple={simple} onClear={openRestart} />
      )}
      {showCtx && !simple && (
        <CtxStrip pct={pct} tokens={p.ctxTokens ?? 0} windowTokens={p.ctxWindow ?? 0} pctOnly={p.ctxPctOnly ?? false} limit={limit} onClear={canRestart ? openRestart : undefined} />
      )}
      {confirming && live && !ended && (
        <span role="group" aria-label="Confirmar encerramento" style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', padding: '6px 8px', borderBottom: '1px solid var(--border)' }}>
          <span style={{ fontSize: 13 }}>Encerrar o agente e o terminal?</span>
          <button type="button" style={btnDanger} onClick={() => { setConfirming(false); term.current?.kill() }}>Sim, encerrar</button>
          <button type="button" style={btn} onClick={() => setConfirming(false)}>Cancelar</button>
        </span>
      )}
      <div style={{ flex: '1 1 auto', minHeight: 0, display: 'flex' }}>
        {live ? (
          <div style={{ flex: 1, minWidth: 0, padding: 4 }}>
            <LazyTerminalView
              ref={term}
              id={t.id}
              grid
              readonly
              active={focused}
              onMode={setRo}
              onStatus={setStatus}
              onEnded={() => setEndedHere(true)}
            />
          </div>
        ) : (
          <button
            type="button"
            onClick={p.onWake}
            style={{ flex: 1, border: 0, background: 'transparent', color: 'var(--text-2)', font: 'inherit', cursor: 'pointer' }}
          >
            Em pausa. Clique para ligar.
          </button>
        )}
      </div>
      {hint && (
        <span role="status" style={{ position: 'absolute', right: 24, bottom: 8, padding: '2px 8px', borderRadius: 6, background: '#000c', fontSize: 12, pointerEvents: 'none' }}>{hint}</span>
      )}
      <button
        type="button"
        aria-label="Redimensionar terminal"
        onPointerDown={onCornerDown}
        onPointerMove={onCornerMove}
        onPointerUp={onCornerUp(false)}
        onPointerCancel={onCornerUp(true)}
        style={{ position: 'absolute', right: 0, bottom: 0, width: 18, height: 18, padding: 0, border: 0, background: 'transparent', color: 'var(--text-2)', cursor: 'nwse-resize', touchAction: 'none', lineHeight: 1 }}
      >◢</button>
    </section>
  )
}

export const GridTile = memo(GridTileView)
