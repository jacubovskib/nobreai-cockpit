import { describe, expect, it } from 'vitest'
import type { TerminalInfo } from '../../api/terminals'
import type { CardSummary, Project } from '../../api/types'
import { chips, clampH, defaultLayout, move, place, reconcile, sizeOf, snapSpan, step, tabs, toggleFull, visible, type Layout } from './gridModel'

const proj = (o: Partial<Project> & Pick<Project, 'id' | 'name'>): Project =>
  ({ path: '', capture: false, base_branch: 'main', parent_id: null, kind: 'repo', ...o })
const projects: Project[] = [
  proj({ id: 1, name: 'NobreTech', kind: 'group' }),
  proj({ id: 2, name: 'cockpit', parent_id: 1, path: '/w/cockpit' }),
  proj({ id: 3, name: 'site', parent_id: 1, path: '/w/site' }),
  proj({ id: 4, name: 'Pessoal', path: '/w/pessoal' }),
]
const term = (id: string, o: Partial<TerminalInfo> = {}): TerminalInfo =>
  ({ id, card_key: '', agent: 'claude', origin: 'web', title: id, pid: 1, started_at: '2026-01-01T00:00:00Z', ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 0, ...o }) as TerminalInfo
const card = (key: string, project_id: number) => ({ key, project_id }) as CardSummary
const layout = (o: Partial<Layout> = {}): Layout => ({ ...defaultLayout(), ...o })

describe('place', () => {
  it('terminal com card segue o projeto do card, sem card usa o dir, sem projeto dá null', () => {
    const r = place(
      [term('a', { card_key: 'NAC-1', dir: '/w/pessoal' }), term('b', { dir: '/w/site/x' }), term('c', { dir: '/outro' }), term('d', { dir: '/w' })],
      projects,
      [card('NAC-1', 2)],
    )
    expect(r.map((x) => [x.rootId, x.subId])).toEqual([[1, 2], [1, 3], [null, null], [null, null]])
  })
})

describe('tabs e chips', () => {
  const placed = place([term('a', { dir: '/w/cockpit' }), term('b', { dir: '/w/pessoal' }), term('c', { dir: '/x' })], projects, [])
  it('tabs: Todos com o total e só raízes com terminal', () => {
    expect(tabs(placed, projects)).toEqual([
      { id: 'all', name: 'Todos', count: 3 },
      { id: 1, name: 'NobreTech', count: 1 },
      { id: 4, name: 'Pessoal', count: 1 },
    ])
  })
  it('chips da raiz com filhos: Todos, Geral só se houver, filhos com terminal', () => {
    expect(chips(placed, projects, 1)).toEqual([{ id: 'all', name: 'Todos', count: 1 }, { id: 2, name: 'cockpit', count: 1 }])
    const comGeral = [...placed, ...place([term('d', { card_key: 'NAC-9' })], projects, [card('NAC-9', 1)])]
    expect(chips(comGeral, projects, 1).map((c) => c.id)).toEqual(['all', 'geral', 2])
  })
  it('chips de raiz sem filhos é vazio', () => {
    expect(chips(placed, projects, 4)).toEqual([])
  })
})

describe('visible', () => {
  const ts = [term('a', { dir: '/w/cockpit' }), term('b', { dir: '/x' }), term('c', { card_key: 'NAC-9' }), term('d', { dir: '/w/site', ended_at: '2026-01-02T00:00:00Z' })]
  const placed = place(ts, projects, [card('NAC-9', 1)])
  it('tab all inclui terminal sem projeto', () => {
    expect(visible(placed, layout(), 'all').map((t) => t.id)).toEqual(['a', 'b', 'c', 'd'])
  })
  it('tab 1 com sub geral só os da raiz', () => {
    expect(visible(placed, layout({ tab: 1, sub: 'geral' }), 'all').map((t) => t.id)).toEqual(['c'])
  })
  it('filtro live tira os encerrados', () => {
    expect(visible(placed, layout(), 'live').map((t) => t.id)).toEqual(['a', 'b', 'c'])
  })
  it('a ordem segue l.order', () => {
    expect(visible(placed, layout({ order: ['d', 'b', 'a'] }), 'all').map((t) => t.id)).toEqual(['d', 'b', 'a', 'c'])
  })
})

describe('reconcile', () => {
  it('remove ids ausentes de order e sizes; novos entram no início na ordem de newestFirst', () => {
    const l = layout({ order: ['a', 'b'], sizes: { a: { span: 2, h: 300 }, b: { span: 1, h: 360 } } })
    const r = reconcile(l, ['a', 'n1', 'n2'], ['n2', 'n1', 'a'])
    expect(r.order).toEqual(['n2', 'n1', 'a'])
    expect(r.sizes).toEqual({ a: { span: 2, h: 300 } })
  })
})

describe('move', () => {
  it('antes do alvo', () => expect(move(['a', 'b', 'c'], 'c', 'a', false)).toEqual(['c', 'a', 'b']))
  it('depois do alvo', () => expect(move(['a', 'b', 'c'], 'a', 'b', true)).toEqual(['b', 'a', 'c']))
  it('para si mesmo não muda', () => expect(move(['a', 'b', 'c'], 'b', 'b', true)).toEqual(['a', 'b', 'c']))
})

describe('tamanhos', () => {
  it('sizeOf limita o span às colunas e dá o padrão ao desconhecido', () => {
    expect(sizeOf(layout({ cols: 2, sizes: { a: { span: 3, h: 400 } } }), 'a')).toEqual({ span: 2, h: 400 })
    expect(sizeOf(layout(), 'x')).toEqual({ span: 1, h: 360 })
  })
  it('snapSpan arredonda e respeita 1..cols', () => {
    expect(snapSpan(900, 440, 12, 3)).toBe(2)
    expect(snapSpan(10, 440, 12, 3)).toBe(1)
    expect(snapSpan(5000, 440, 12, 3)).toBe(3)
  })
  it('clampH', () => {
    expect(clampH(50)).toBe(200)
    expect(clampH(2000)).toBe(900)
  })
  it('step soma span e altura, com limites', () => {
    expect(step({ span: 1, h: 360 }, 1, 3)).toEqual({ span: 2, h: 450 })
    expect(step({ span: 1, h: 200 }, -1, 3)).toEqual({ span: 1, h: 200 })
  })
  it('toggleFull vai e volta', () => {
    const full = toggleFull({ span: 1, h: 360 }, 3)
    expect(full).toEqual({ span: 3, h: 520 })
    expect(toggleFull(full, 3)).toEqual({ span: 1, h: 360 })
  })
})
