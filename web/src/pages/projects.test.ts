import { buildRows, contextLine, stateLabel, timeline } from './projects'
import type { CardSummary, Project, Total } from '../api/types'

const T = '2026-11-10T12:00:00Z'
const iso = (h: number) => new Date(Date.parse(T) - h * 3600000).toISOString()
const card = (o: Partial<CardSummary>) => ({
  id: 1, key: 'NAC-1', title: 'x', column: 'backlog', project_id: 1, created_at: iso(500), updated_at: iso(500),
  last_activity_at: null, signal: '', signal_since: null, ...o,
}) as CardSummary
const proj = (id: number, capture = true): Project => ({ id, name: `p${id}`, path: `/w/${id}`, capture, base_branch: "", parent_id: null, kind: "repo" })
const tot = (key: string, cost: number) => ({ key, cost_usd: cost, unpriced: false }) as Total

describe('buildRows', () => {
  const cards = [
    card({ id: 1, key: 'A', column: 'em_andamento', last_activity_at: iso(1), updated_at: iso(2) }),
    card({ id: 2, key: 'B', column: 'pausado', signal: 'parado', signal_since: iso(30), last_activity_at: iso(30), updated_at: iso(30) }),
    card({ id: 3, key: 'C', column: 'revisao', project_id: 2, signal: 'aguardando_revisao', signal_since: iso(40), updated_at: iso(40) }),
  ]
  it('uma linha por projeto, sem "Sem projeto" quando o custo é 0', () => {
    const rows = buildRows([proj(2), proj(1)], cards, [tot('1', 41), tot('2', 8), tot('', 0)])
    expect(rows).toHaveLength(2)
    expect(rows[0].project?.id).toBe(1)
    expect(rows[0].current?.key).toBe('A')
    expect(rows[0].stale.count).toBe(1)
    expect(rows[0].stale.oldest).toBe(iso(30))
    expect(rows[0].costUsd).toBe(41)
    expect(rows[1].awaiting.count).toBe(1)
    expect(rows[1].current?.key).toBe('C')
    expect(rows[0].lastActivity).toBe(iso(1))
  })
  it('com custo sem projeto, acrescenta a linha no fim', () => {
    const rows = buildRows([proj(1), proj(2)], cards, [tot('', 3)])
    expect(rows).toHaveLength(3)
    expect(rows[2].project).toBeNull()
    expect(rows[2].costUsd).toBe(3)
  })
  it('projeto sem captura continua com linha', () => {
    const rows = buildRows([proj(1, false)], [], [])
    expect(rows).toHaveLength(1)
    expect(rows[0].project?.capture).toBe(false)
    expect(rows[0].current).toBeNull()
    expect(rows[0].lastActivity).toBeNull()
  })
})

describe('timeline', () => {
  it('ordena por grupo', () => {
    const cards = [
      card({ id: 1, key: 'BL', column: 'backlog', created_at: iso(10) }),
      card({ id: 2, key: 'PA', column: 'pausado', updated_at: iso(5) }),
      card({ id: 3, key: 'RV', column: 'revisao', signal: 'aguardando_revisao', signal_since: iso(30), updated_at: iso(30) }),
      card({ id: 4, key: 'PD', column: 'em_andamento', signal: 'parado', signal_since: iso(60), updated_at: iso(60) }),
      card({ id: 5, key: 'C2', column: 'concluido', updated_at: iso(1) }),
      card({ id: 6, key: 'C1', column: 'concluido', updated_at: iso(100) }),
      card({ id: 7, key: 'EA', column: 'em_andamento', updated_at: iso(2) }),
      card({ id: 8, key: 'PS', column: 'pausado', signal: 'parado', signal_since: iso(40), updated_at: iso(40) }),
    ]
    expect(timeline(cards).map((e) => `${e.group}:${e.card.key}`)).toEqual([
      'concluidas:C1', 'concluidas:C2',
      'andamento:EA', 'andamento:PA', 'andamento:RV',
      'paradas:PD', 'paradas:PS',
      'backlog:BL',
    ])
  })
})

describe('buildRows com família', () => {
  const loja = { ...proj(1), name: 'loja' }
  const web = { ...proj(2), name: 'web', parent_id: 1 }
  const api = { ...proj(3), name: 'api', parent_id: 1 }
  it('family total equals children plus general', () => {
    const cards = [card({ id: 1, key: 'A', project_id: 2, column: 'em_andamento', updated_at: iso(1) }), card({ id: 2, key: 'B', project_id: 1 })]
    const rows = buildRows([loja, web, api], cards, [tot('1', 26.0), tot('2', 95.1), tot('3', 61.05)])
    expect(rows).toHaveLength(1)
    const [r] = rows
    expect(r.costUsd).toBe(182.15)
    expect(r.children.map((c) => c.project?.name)).toEqual(['api', 'web'])
    expect(r.general?.costUsd).toBe(26)
    expect(r.children.reduce((s, c) => s + c.costUsd, 0) + (r.general?.costUsd ?? 0)).toBeCloseTo(r.costUsd, 10)
    expect(r.current?.key).toBe('A')
  })
})

describe('contextLine e stateLabel', () => {
  const task = (n: number, status: string, title = `t${n}`) => ({ n, title, status, session_id: '', updated_at: T })
  const withPlan = (tasks: ReturnType<typeof task>[], o: Partial<CardSummary> = {}) => card({ column: 'em_andamento', plan: { path: 'docs/plan.md', tasks } as never, ...o })
  it('concluído mostra a data', () => {
    expect(contextLine(card({ column: 'concluido', updated_at: '2026-11-03T12:00:00Z' }))).toBe('concluída 03/11')
  })
  it('sem plano é vazio', () => {
    expect(contextLine(card({ column: 'em_andamento', plan: null }))).toBe('')
  })
  it('tarefa em andamento ou em revisão', () => {
    expect(contextLine(withPlan([task(1, 'concluida'), task(2, 'em_revisao', 'filtros no board')]))).toBe('tarefa 2: filtros no board (em revisão)')
    expect(contextLine(withPlan([task(1, 'concluida'), task(2, 'em_andamento', 'api')]))).toBe('tarefa 2: api')
  })
  it('todas a fazer', () => {
    expect(contextLine(withPlan([task(1, 'a_fazer'), task(2, 'a_fazer')]))).toBe('plano escrito, não iniciado')
  })
  it('todas concluídas com o card em andamento', () => {
    expect(contextLine(withPlan([task(1, 'concluida'), task(2, 'concluida')]))).toBe('tarefas concluídas')
  })
  it('próxima tarefa', () => {
    expect(contextLine(withPlan([task(1, 'concluida'), task(2, 'a_fazer', 'docs'), task(3, 'a_fazer')]))).toBe('próxima: tarefa 2: docs')
  })
  it('stateLabel', () => {
    expect(stateLabel(card({ column: 'concluido' }))).toBe('concluída')
    expect(stateLabel(card({ column: 'pausado', signal: 'parado', signal_since: iso(50) }))).toMatch(/^parado há/)
    expect(stateLabel(card({ column: 'revisao', signal: 'aguardando_revisao', signal_since: iso(50) }))).toMatch(/^aguardando revisão há/)
    expect(stateLabel(card({ column: 'backlog' }))).toBe('backlog')
    expect(stateLabel(card({ column: 'em_andamento' }))).toBe('em andamento')
  })
})
