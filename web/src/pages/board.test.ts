import { groupByColumn, COLUMNS, signalText } from './board'
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

describe('signalText', () => {
  const now = new Date('2026-05-10T12:00:00Z')
  const ago = (h: number) => new Date(now.getTime() - h * 3600000).toISOString()
  it('mostra horas abaixo de 48 h e dias a partir daí', () => {
    expect(signalText('parado', ago(30), now)).toBe('parado há 30 h')
    expect(signalText('parado', ago(50), now)).toBe('parado há 2 dias')
    expect(signalText('aguardando_revisao', ago(73), now)).toBe('aguardando revisão há 3 dias')
  })
  it('sem sinal devolve vazio', () => {
    expect(signalText('', ago(100), now)).toBe('')
    expect(signalText('parado', null, now)).toBe('')
  })
})
