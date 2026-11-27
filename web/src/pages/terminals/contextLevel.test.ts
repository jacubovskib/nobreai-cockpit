import { describe, expect, it } from 'vitest'
import type { TerminalInfo } from '../../api/terminals'
import type { ActiveSession, LiveContextSummary } from '../../api/types'
import { ctxDetail, ctxLevel, ctxLimit, ctxWarning, tileMeter } from './contextLevel'

const term = { id: 't1', card_key: 'NAC-1' } as TerminalInfo
const sess = (card: { key: string; where_stopped: string; where_stopped_at: string | null } | null) =>
  ({ id: 's1', effective_project_id: 7, card }) as unknown as Pick<ActiveSession, 'id' | 'card' | 'effective_project_id'>
const live = (o: Partial<LiveContextSummary>) => ({ pct: 0, tokens: 0, window_tokens: 0, ...o }) as LiveContextSummary

describe('ctxLimit', () => {
  it('usa a última faixa', () => {
    expect(ctxLimit([60, 80])).toBe(80)
    expect(ctxLimit([70])).toBe(70)
  })
  it('lista vazia, ausente ou inválida volta a 80', () => {
    expect(ctxLimit([])).toBe(80)
    expect(ctxLimit(undefined)).toBe(80)
    expect(ctxLimit('x' as never)).toBe(80)
  })
})

describe('ctxLevel', () => {
  it('verde, âmbar e vermelho', () => {
    expect(ctxLevel(79, 80)).toBe('ok')
    expect(ctxLevel(80, 80)).toBe('warn')
    expect(ctxLevel(89, 80)).toBe('warn')
    expect(ctxLevel(90, 80)).toBe('crit')
  })
  it('limite acima de 90 move o crítico', () => {
    expect(ctxLevel(92, 95)).toBe('ok')
    expect(ctxLevel(95, 95)).toBe('crit')
    expect(ctxLevel(90, 90)).toBe('crit')
  })
})

describe('textos', () => {
  it('ctxDetail', () => {
    expect(ctxDetail(124000, 200000, false)).toBe('124 mil de 200 mil')
    expect(ctxDetail(0, 0, true)).toBe('o agente informa só a %')
  })
  it('ctxWarning', () => {
    expect(ctxWarning(84, 80, false)).toBe('Contexto em 84%, acima do limite de 80%. Limpe e continue de onde parou.')
    expect(ctxWarning(84, 80, true)).toBe('Contexto em 84%. Limpe e continue de onde parou.')
  })
})

describe('tileMeter', () => {
  it('sem sessão', () => {
    expect(tileMeter(term, undefined, undefined)).toEqual({
      sessionId: '', cardKey: 'NAC-1', projectId: null, ctxPct: null,
      ctxTokens: 0, ctxWindow: 0, ctxPctOnly: false, whereStopped: '', whereStoppedAt: null,
    })
  })
  it('sessão com card e contexto', () => {
    const s = sess({ key: 'NAC-9', where_stopped: 'no passo 3', where_stopped_at: '2026-01-01T00:00:00Z' })
    expect(tileMeter(term, s, live({ pct: 61.6, tokens: 124000, window_tokens: 200000 }))).toEqual({
      sessionId: 's1', cardKey: 'NAC-9', projectId: 7, ctxPct: 62,
      ctxTokens: 124000, ctxWindow: 200000, ctxPctOnly: false,
      whereStopped: 'no passo 3', whereStoppedAt: '2026-01-01T00:00:00Z',
    })
  })
  it('sessão sem card não herda o card do terminal', () => {
    const m = tileMeter(term, sess(null), live({ pct: 10, tokens: 1000, window_tokens: 200000 }))
    expect(m.cardKey).toBe('')
    expect(m.sessionId).toBe('s1')
  })
  it('sem turno medido não tem percentual', () => {
    expect(tileMeter(term, sess(null), live({ tokens: 0, pct: 0 })).ctxPct).toBeNull()
  })
  it('pct_only mantém o percentual', () => {
    const m = tileMeter(term, sess(null), live({ pct: 62, pct_only: true }))
    expect(m.ctxPct).toBe(62)
    expect(m.ctxPctOnly).toBe(true)
  })
  it('percentual acima de 100 segue como veio', () => {
    expect(tileMeter(term, sess(null), live({ pct: 104.2, tokens: 5, window_tokens: 10 })).ctxPct).toBe(104)
  })
})
