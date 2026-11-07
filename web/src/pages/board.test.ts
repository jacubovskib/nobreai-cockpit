import { groupByColumn, COLUMNS } from './board'
import type { CardSummary } from '../api/types'

describe('groupByColumn', () => {
  it('devolve as 5 colunas na ordem, mesmo sem cards', () => {
    const g = groupByColumn([])
    expect(Object.keys(g)).toEqual(['backlog', 'em_andamento', 'pausado', 'revisao', 'concluido'])
    expect(COLUMNS.map((c) => c.id)).toEqual(Object.keys(g))
    for (const k of Object.keys(g)) expect(g[k as keyof typeof g]).toEqual([])
  })
  it('agrupa cada card na sua coluna', () => {
    const g = groupByColumn([
      { key: 'NAC-1', column: 'pausado' } as CardSummary,
      { key: 'NAC-2', column: 'backlog' } as CardSummary,
      { key: 'NAC-3', column: 'pausado' } as CardSummary,
    ])
    expect(g.pausado.map((c) => c.key)).toEqual(['NAC-1', 'NAC-3'])
    expect(g.backlog).toHaveLength(1)
  })
})
