import { groupByColumn, COLUMNS, cardMarks, signalText, applyBoardFilter, epicOptions, matchesSearch, handoffHint, readyToResume, boardStrip, ageText, recentDone, isBareBacklog } from './boardModel'
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
  it('epicOptions inclui o épico citado pelas filhas visíveis e o selecionado sem dados', () => {
    const filhas = [c({ key: 'NAC-71', parent_key: 'NAC-70', parent_title: 'De outro projeto' }), c({ key: 'NAC-72', parent_key: 'NAC-70', parent_title: 'De outro projeto' })]
    expect(epicOptions(filhas)).toEqual([{ key: 'NAC-70', title: 'De outro projeto' }])
    expect(epicOptions(filhas, 'NAC-99')).toEqual([{ key: 'NAC-70', title: 'De outro projeto' }, { key: 'NAC-99', title: '' }])
    expect(epicOptions(cards, 'none')).toHaveLength(1)
  })
})

describe('busca do board', () => {
  const c = (o: object) => ({ type: 'tarefa', parent_key: '', waiting_on_you: false, ...o }) as CardSummary
  const cards = [
    c({ key: 'NAC-5', title: 'Visão por projeto' }),
    c({ key: 'NAC-50', title: 'Sessões no lugar de Terminais', type: 'bug' }),
    c({ key: 'NAC-7', title: 'README e Makefile' }),
  ]
  const keys = (l: CardSummary[]) => l.map((x) => x.key)
  it('casa por chave e por título, sem diferenciar maiúsculas nem acentos', () => {
    expect(matchesSearch(cards[0], 'nac-5')).toBe(true)
    expect(matchesSearch(cards[0], 'VISAO')).toBe(true)
    expect(matchesSearch(cards[1], 'sessoes')).toBe(true)
    expect(matchesSearch(cards[2], 'visão')).toBe(false)
  })
  it('busca vazia ou só com espaços casa tudo', () => {
    expect(matchesSearch(cards[2], '')).toBe(true)
    expect(matchesSearch(cards[2], '   ')).toBe(true)
  })
  it('applyBoardFilter aplica a busca e combina com o tipo', () => {
    const none = { epic: '', type: '', waiting: false } as const
    expect(keys(applyBoardFilter(cards, { ...none, search: 'nac-5' }))).toEqual(['NAC-5', 'NAC-50'])
    expect(keys(applyBoardFilter(cards, { ...none, search: 'nac-5', type: 'bug' }))).toEqual(['NAC-50'])
    expect(keys(applyBoardFilter(cards, { ...none, search: 'makefile' }))).toEqual(['NAC-7'])
    expect(applyBoardFilter(cards, none)).toHaveLength(3)
  })
})

describe('indicador de handoff', () => {
  const r = (met: number, total = 5) => ({ met, total, ready: total > 0 && met === total, items: [] })
  const c = (o: object) => ({ type: 'tarefa', column: 'pausado', session_count: 1, live: false, signal: '', readiness: r(3), ...o }) as unknown as CardSummary
  it('pausado com handoff completo: pronto para retomar', () => {
    expect(handoffHint(c({ readiness: r(5) }))).toBe('pronto para retomar')
    expect(readyToResume(c({ readiness: r(5) }))).toBe(true)
  })
  it('completo fora de Pausado não aparece', () => {
    for (const column of ['backlog', 'em_andamento', 'revisao', 'concluido']) {
      expect(handoffHint(c({ column, readiness: r(5) }))).toBe('')
      expect(readyToResume(c({ column, readiness: r(5) }))).toBe(false)
    }
  })
  it('pausado completo mas bloqueado não é pronto para retomar', () => {
    expect(handoffHint(c({ readiness: r(5), signal: 'bloqueado' }))).toBe('')
    expect(readyToResume(c({ readiness: r(5), signal: 'bloqueado' }))).toBe(false)
  })
  it('parcial em card com sessão: contexto n/5 (inclusive 0/5)', () => {
    expect(handoffHint(c({ readiness: r(3) }))).toBe('contexto 3/5')
    expect(handoffHint(c({ readiness: r(0) }))).toBe('contexto 0/5')
    expect(handoffHint(c({ column: 'em_andamento', session_count: 0, live: true, readiness: r(2) }))).toBe('contexto 2/5')
    expect(handoffHint(c({ column: 'revisao', readiness: r(4) }))).toBe('contexto 4/5')
  })
  it('sem sessão, concluído, épico ou sem regras: nada', () => {
    expect(handoffHint(c({ column: 'backlog', session_count: 0, readiness: r(1) }))).toBe('')
    expect(handoffHint(c({ column: 'pausado', session_count: 0, readiness: r(1) }))).toBe('')
    expect(handoffHint(c({ column: 'concluido', readiness: r(3) }))).toBe('')
    expect(handoffHint(c({ type: 'epico', readiness: r(3) }))).toBe('')
    expect(handoffHint(c({ readiness: r(0, 0) }))).toBe('')
  })
})

describe('faixa do que espera por você', () => {
  const now = new Date('2026-05-10T12:00:00Z')
  const ago = (h: number) => new Date(now.getTime() - h * 3600000).toISOString()
  const r = (met: number) => ({ met, total: 5, ready: met === 5, items: [] })
  const c = (o: object) => ({ type: 'tarefa', column: 'backlog', session_count: 0, live: false, signal: '', signal_since: null, readiness: r(0), ...o }) as unknown as CardSummary
  it('conta revisão, pausados prontos e agentes ao vivo', () => {
    const s = boardStrip([
      c({ column: 'revisao', signal: 'aguardando_revisao', signal_since: ago(36) }),
      c({ column: 'revisao', signal: 'aguardando_revisao', signal_since: ago(73) }),
      c({ column: 'revisao' }),
      c({ column: 'pausado', session_count: 1, readiness: r(5) }),
      c({ column: 'pausado', session_count: 1, readiness: r(3) }),
      c({ column: 'pausado', session_count: 1, readiness: r(5), signal: 'bloqueado' }),
      c({ column: 'em_andamento', live: true }),
      c({ column: 'concluido' }),
    ])
    expect(s).toEqual({ review: 3, oldestReview: ago(73), resumable: 1, live: 1 })
  })
  it('sem sinal de revisão não há idade', () => {
    expect(boardStrip([c({ column: 'revisao' })])).toEqual({ review: 1, oldestReview: null, resumable: 0, live: 0 })
    // signal_since de outro sinal não vale como idade da revisão
    expect(boardStrip([c({ column: 'pausado', signal: 'parado', signal_since: ago(90) })]).oldestReview).toBeNull()
  })
  it('lista vazia zera tudo', () => {
    expect(boardStrip([])).toEqual({ review: 0, oldestReview: null, resumable: 0, live: 0 })
  })
  it('ageText: horas abaixo de 48 h, dias a partir daí, vazio sem data', () => {
    expect(ageText(ago(36), now)).toBe('há 36 h')
    expect(ageText(ago(73), now)).toBe('há 3 dias')
    expect(ageText(null, now)).toBe('')
    expect(ageText('lixo', now)).toBe('')
  })
})

describe('corte do Concluído e backlog enxuto', () => {
  const c = (o: object) => ({ type: 'tarefa', column: 'concluido', session_count: 0, live: false, ...o }) as CardSummary
  const keys = (l: CardSummary[]) => l.map((x) => x.key)
  it('recentDone ordena pelo mais recente e corta em 3', () => {
    const l = [
      c({ key: 'A', updated_at: '2026-05-01T00:00:00Z' }),
      c({ key: 'B', updated_at: '2026-05-04T00:00:00Z' }),
      c({ key: 'C', updated_at: '2026-05-02T00:00:00Z' }),
      c({ key: 'D', updated_at: '2026-05-03T00:00:00Z' }),
    ]
    expect(keys(recentDone(l))).toEqual(['B', 'D', 'C'])
    expect(keys(recentDone(l, Infinity))).toEqual(['B', 'D', 'C', 'A'])
    expect(keys(l)).toEqual(['A', 'B', 'C', 'D']) // não altera a entrada
  })
  it('recentDone tolera data ausente: mantém a ordem e deixa sem data por último', () => {
    expect(keys(recentDone([c({ key: 'A' }), c({ key: 'B' }), c({ key: 'C', updated_at: '2026-05-02T00:00:00Z' }), c({ key: 'D' })])))
      .toEqual(['C', 'A', 'B'])
  })
  it('isBareBacklog: backlog sem sessão e não-épico', () => {
    expect(isBareBacklog(c({ column: 'backlog' }))).toBe(true)
    expect(isBareBacklog(c({ column: 'backlog', session_count: 1 }))).toBe(false)
    expect(isBareBacklog(c({ column: 'backlog', live: true }))).toBe(false)
    expect(isBareBacklog(c({ column: 'backlog', type: 'epico' }))).toBe(false)
    expect(isBareBacklog(c({ column: 'pausado' }))).toBe(false)
  })
})
