// Modelo da grade de terminais: lógica pura, sem React.
import { isLive, type TerminalInfo } from '../../api/terminals'
import type { CardSummary, Project } from '../../api/types'
import { projectForPath } from '../../app/project'

export const DEF_H = 360, MIN_H = 200, MAX_H = 900, DEF_COLS = 3
const FULL_H = 520
const STEP_H = 90

export type Cols = 1 | 2 | 3
export type StateFilter = 'all' | 'live'
export interface TileSize { span: number; h: number }
export interface Layout { order: string[]; sizes: Record<string, TileSize>; cols: Cols; simple: boolean; tab: number | 'all'; sub: number | 'all' | 'geral' }

export const defaultLayout = (): Layout => ({ order: [], sizes: {}, cols: DEF_COLS, simple: false, tab: 'all', sub: 'all' })

// subId null = o terminal está direto no projeto raiz (ou sem projeto).
export interface Placed { t: TerminalInfo; rootId: number | null; subId: number | null }

// place: terminal com card segue o projeto do card; só sem card vale o diretório.
export function place(terms: TerminalInfo[], projects: Project[], cards: CardSummary[]): Placed[] {
  return terms.map((t) => {
    const card = t.card_key ? cards.find((c) => c.key === t.card_key) : undefined
    const project = card ? projects.find((p) => p.id === card.project_id) ?? null : t.dir ? projectForPath(projects, t.dir) : null
    if (!project) return { t, rootId: null, subId: null }
    // Raiz: sobe por parent_id até null (com trava contra ciclo).
    let root = project
    for (let i = 0; i < projects.length && root.parent_id != null; i++) {
      const parent = projects.find((p) => p.id === root.parent_id)
      if (!parent) break
      root = parent
    }
    return { t, rootId: root.id, subId: root.id === project.id ? null : project.id }
  })
}

export interface Tab { id: number | 'all'; name: string; count: number }
export interface Chip { id: number | 'all' | 'geral'; name: string; count: number }

// tabs: "Todos" mais as raízes que têm terminal.
export function tabs(placed: Placed[], projects: Project[]): Tab[] {
  const out: Tab[] = [{ id: 'all', name: 'Todos', count: placed.length }]
  for (const p of projects) {
    if (p.parent_id != null) continue
    const count = placed.filter((x) => x.rootId === p.id).length
    if (count > 0) out.push({ id: p.id, name: p.name, count })
  }
  return out
}

// chips: sub-filtros da raiz escolhida; vazio se ela não tem filhos.
export function chips(placed: Placed[], projects: Project[], tab: number | 'all'): Chip[] {
  if (tab === 'all') return []
  const children = projects.filter((p) => p.parent_id === tab)
  if (children.length === 0) return []
  const inRoot = placed.filter((x) => x.rootId === tab)
  const out: Chip[] = [{ id: 'all', name: 'Todos', count: inRoot.length }]
  const geral = inRoot.filter((x) => x.subId === null).length
  if (geral > 0) out.push({ id: 'geral', name: 'Geral', count: geral })
  for (const c of children) {
    const count = inRoot.filter((x) => x.subId === c.id).length
    if (count > 0) out.push({ id: c.id, name: c.name, count })
  }
  return out
}

// visible: filtrado por aba, sub-filtro e estado, na ordem de l.order (ids fora dela ficam no fim).
export function visible(placed: Placed[], l: Layout, f: StateFilter): TerminalInfo[] {
  const list = placed.filter((x) => {
    if (l.tab !== 'all' && x.rootId !== l.tab) return false
    if (l.sub === 'geral' && x.subId !== null) return false
    if (typeof l.sub === 'number' && x.subId !== l.sub) return false
    return f === 'all' || isLive(x.t)
  })
  const pos = new Map(l.order.map((id, i) => [id, i]))
  const rank = (id: string) => pos.get(id) ?? Infinity
  return list
    .map((x, i) => ({ t: x.t, i }))
    .sort((a, b) => {
      const ra = rank(a.t.id), rb = rank(b.t.id)
      return ra === rb ? a.i - b.i : ra < rb ? -1 : 1
    })
    .map((x) => x.t)
}

// reconcile: tira ids mortos e põe os novos no início, na ordem de newestFirst.
export function reconcile(l: Layout, ids: string[], newestFirst: string[]): Layout {
  const alive = new Set(ids)
  const known = new Set(l.order)
  const fresh = newestFirst.filter((id) => alive.has(id) && !known.has(id))
  const sizes: Record<string, TileSize> = {}
  for (const [id, s] of Object.entries(l.sizes)) if (alive.has(id)) sizes[id] = s
  return { ...l, order: [...fresh, ...l.order.filter((id) => alive.has(id))], sizes }
}

// move: tira o id e recoloca antes (after=false) ou depois do alvo.
export function move(order: string[], id: string, target: string, after: boolean): string[] {
  if (id === target || !order.includes(id) || !order.includes(target)) return order
  const rest = order.filter((x) => x !== id)
  const i = rest.indexOf(target)
  rest.splice(after ? i + 1 : i, 0, id)
  return rest
}

export function sizeOf(l: Layout, id: string): TileSize {
  const s = l.sizes[id] ?? { span: 1, h: DEF_H }
  return { span: Math.max(1, Math.min(s.span, l.cols)), h: s.h }
}

// snapSpan: quantas colunas cabem na largura arrastada.
export function snapSpan(widthPx: number, colW: number, gap: number, cols: Cols): number {
  const n = Math.round((widthPx + gap) / (colW + gap))
  return Math.max(1, Math.min(cols, n))
}

export const clampH = (h: number): number => Math.max(MIN_H, Math.min(MAX_H, h))

export function step(s: TileSize, dir: 1 | -1, cols: Cols): TileSize {
  return { span: Math.max(1, Math.min(cols, s.span + dir)), h: clampH(s.h + dir * STEP_H) }
}

// toggleFull: alterna entre tela cheia da grade e o tamanho padrão.
export function toggleFull(s: TileSize, cols: Cols): TileSize {
  return s.span >= cols && s.h === FULL_H ? { span: 1, h: DEF_H } : { span: cols, h: FULL_H }
}
