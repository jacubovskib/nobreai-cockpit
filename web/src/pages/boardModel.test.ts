import { groupByColumn, COLUMNS, cardMarks, signalText, applyBoardFilter, epicOptions } from './boardModel'
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
    expect(signalText({ signal: 'parado', signal_since: ago(30), blocked_by: '' }, now)).toBe('parado há 30 h')
    expect(signalText({ signal: 'parado', signal_since: ago(50), blocked_by: '' }, now)).toBe('parado há 2 dias')
    expect(signalText({ signal: 'aguardando_revisao', signal_since: ago(73), blocked_by: '' }, now)).toBe('aguardando revisão há 3 dias')
  })
  it('mostra os sinais de pausa', () => {
    expect(signalText({ signal: 'bloqueado', signal_since: ago(5), blocked_by: 'NAC-4' }, now)).toBe('bloqueado por NAC-4')
    expect(signalText({ signal: 'pronto_para_retomar', signal_since: ago(5), blocked_by: 'NAC-4' }, now)).toBe('pronto para retomar (NAC-4 concluído)')
    expect(signalText({ signal: 'aguardando_voce', signal_since: ago(5), blocked_by: '' }, now)).toBe('aguardando você há 5 h')
    expect(signalText({ signal: 'aguardando_voce', signal_since: ago(73), blocked_by: '' }, now)).toBe('aguardando você há 3 dias')
  })
  it('sem sinal devolve vazio', () => {
    expect(signalText({ signal: '', signal_since: ago(100), blocked_by: '' }, now)).toBe('')
    expect(signalText({ signal: 'parado', signal_since: null, blocked_by: '' }, now)).toBe('')
  })
})

describe('cardMarks', () => {
  it('handoff incompleto só enquanto o card está em Pausado e não pronto', () => {
    const c = { column: 'pausado' as const, pause_reason: '', handoff_incomplete: true }
    expect(cardMarks(c, false)).toEqual(['handoff incompleto'])
    expect(cardMarks(c, true)).toEqual([])
    for (const column of ['backlog', 'em_andamento', 'revisao', 'concluido'] as const) {
      expect(cardMarks({ ...c, column }, false)).toEqual([])
    }
  })
  it('limite de custo', () => {
    expect(cardMarks({ column: 'pausado', pause_reason: 'limite de custo', handoff_incomplete: false }, false)).toEqual(['pausado por limite de custo'])
  })
})

describe('applyBoardFilter e epicOptions', () => {
  const c = (o: object) => ({ type: 'tarefa', parent_key: '', waiting_on_you: false, ...o }) as CardSummary
  const cards = [
    c({ key: 'NAC-60', type: 'epico', title: 'Checkout novo' }),
    c({ key: 'NAC-61', parent_key: 'NAC-60' }),
    c({ key: 'NAC-62', parent_key: 'NAC-60', type: 'bug', waiting_on_you: true }),
    c({ key: 'NAC-63' }),
    c({ key: 'NAC-64', type: 'bug' }),
  ]
  const none = { epic: '', type: '', waiting: false } as const
  const keys = (l: CardSummary[]) => l.map((x) => x.key)
  it('sem filtro devolve tudo', () => expect(applyBoardFilter(cards, none)).toHaveLength(5))
  it('épico mantém o épico e as filhas', () => {
    expect(keys(applyBoardFilter(cards, { ...none, epic: 'NAC-60' }))).toEqual(['NAC-60', 'NAC-61', 'NAC-62'])
  })
  it('none mantém só quem não tem pai e não é épico', () => {
    expect(keys(applyBoardFilter(cards, { ...none, epic: 'none' }))).toEqual(['NAC-63', 'NAC-64'])
  })
  it('filtra por tipo e por esperando você, e combina', () => {
    expect(keys(applyBoardFilter(cards, { ...none, type: 'bug' }))).toEqual(['NAC-62', 'NAC-64'])
    expect(keys(applyBoardFilter(cards, { ...none, waiting: true }))).toEqual(['NAC-62'])
    expect(keys(applyBoardFilter(cards, { epic: 'NAC-60', type: 'bug', waiting: true }))).toEqual(['NAC-62'])
  })
  it('a entrada já filtrada por projeto sem o épico não o inventa', () => {
    expect(keys(applyBoardFilter(cards.slice(1), { ...none, epic: 'NAC-60' }))).toEqual(['NAC-61', 'NAC-62'])
  })
  it('epicOptions lista só os épicos', () => {
    expect(epicOptions(cards)).toEqual([{ key: 'NAC-60', title: 'Checkout novo' }])
  })
})
