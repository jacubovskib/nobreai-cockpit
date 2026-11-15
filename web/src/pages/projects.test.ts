import { buildRows, timeline } from './projects'
import type { CardSummary, Project, Total } from '../api/types'

const T = '2026-11-10T12:00:00Z'
const iso = (h: number) => new Date(Date.parse(T) - h * 3600000).toISOString()
const card = (o: Partial<CardSummary>) => ({
  id: 1, key: 'NAC-1', title: 'x', column: 'backlog', project_id: 1, created_at: iso(500), updated_at: iso(500),
  last_activity_at: null, signal: '', signal_since: null, ...o,
}) as CardSummary
const proj = (id: number, capture = true): Project => ({ id, name: `p${id}`, path: `/w/${id}`, capture, base_branch: "" })
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
