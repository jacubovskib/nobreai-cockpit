import { describe, expect, it } from 'vitest'
import type { TerminalInfo } from '../../api/terminals'
import { matchSessions, sessionOfTerminal, terminalOfSession, type SessionRef } from './sessionMatch'

const term = (o: Partial<TerminalInfo> & { id: string; pid: number }): TerminalInfo =>
  ({ card_key: '', agent: 'claude', origin: 'web', title: '', started_at: '2026-01-01T00:00:00Z', ended_at: null, exit_code: null, cols: 80, rows: 24, ...o }) as TerminalInfo

const sess = (o: Partial<SessionRef> & { id: string; pid: number }): SessionRef =>
  ({ started_at: '2026-01-01T00:00:00Z', ended_at: null, pid_dead_at: null, ...o })

describe('casamento terminal → sessão', () => {
  it('pid igual casa', () => {
    const s = sess({ id: 's1', pid: 100 })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [s])).toBe(s)
  })

  it('sessão com ended_at é ignorada', () => {
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [sess({ id: 's1', pid: 100, ended_at: '2026-01-01T01:00:00Z' })])).toBeUndefined()
  })

  it('sessão com pid_dead_at é ignorada', () => {
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [sess({ id: 's1', pid: 100, pid_dead_at: '2026-01-01T01:00:00Z' })])).toBeUndefined()
  })

  it('terminal encerrado não casa', () => {
    expect(sessionOfTerminal(term({ id: 't1', pid: 100, ended_at: '2026-01-01T01:00:00Z' }), [sess({ id: 's1', pid: 100 })])).toBeUndefined()
  })

  it('duas sessões vivas no mesmo pid: a de started_at mais recente', () => {
    const velha = sess({ id: 'velha', pid: 100, started_at: '2026-01-01T00:00:00Z' })
    const nova = sess({ id: 'nova', pid: 100, started_at: '2026-01-01T02:00:00Z' })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [nova, velha])?.id).toBe('nova')
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [velha, nova])?.id).toBe('nova')
  })

  it('empate de started_at: a última da lista', () => {
    const a = sess({ id: 'a', pid: 100 })
    const b = sess({ id: 'b', pid: 100 })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [a, b])?.id).toBe('b')
  })

  it('sessão reaberta, de início mais antigo e atividade mais recente, vence', () => {
    // O processo voltou a uma conversa antiga (reaberta: o started_at fica o de antes).
    const abandonada = sess({ id: 'abandonada', pid: 100, started_at: '2026-01-01T10:00:00Z', last_event_at: '2026-01-01T10:30:00Z' })
    const reaberta = sess({ id: 'reaberta', pid: 100, started_at: '2025-12-31T09:00:00Z', last_event_at: '2026-01-01T11:00:00Z' })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [abandonada, reaberta])?.id).toBe('reaberta')
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [reaberta, abandonada])?.id).toBe('reaberta')
  })

  it('empate de last_event_at: a de started_at mais recente', () => {
    const velha = sess({ id: 'velha', pid: 100, started_at: '2026-01-01T00:00:00Z', last_event_at: '2026-01-01T05:00:00Z' })
    const nova = sess({ id: 'nova', pid: 100, started_at: '2026-01-01T02:00:00Z', last_event_at: '2026-01-01T05:00:00Z' })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [nova, velha])?.id).toBe('nova')
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [velha, nova])?.id).toBe('nova')
  })

  it('last_event_at ausente ou vazio vale o started_at da própria sessão', () => {
    // Sem atividade registrada, a sessão recém-aberta conta pelo início: vence a de atividade mais antiga.
    const antiga = sess({ id: 'antiga', pid: 100, started_at: '2026-01-01T00:00:00Z', last_event_at: '2026-01-01T01:00:00Z' })
    const semData = sess({ id: 'sem-data', pid: 100, started_at: '2026-01-01T02:00:00Z' })
    const vazia = sess({ id: 'vazia', pid: 100, started_at: '2026-01-01T02:00:00Z', last_event_at: '' })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [semData, antiga])?.id).toBe('sem-data')
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [vazia, antiga])?.id).toBe('vazia')
    // E perde para uma atividade mais recente que o próprio início.
    const ativa = sess({ id: 'ativa', pid: 100, started_at: '2026-01-01T00:00:00Z', last_event_at: '2026-01-01T03:00:00Z' })
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [ativa, semData])?.id).toBe('ativa')
  })

  it('pid 0 na sessão ou no terminal nunca casa', () => {
    expect(sessionOfTerminal(term({ id: 't1', pid: 100 }), [sess({ id: 's1', pid: 0 })])).toBeUndefined()
    expect(sessionOfTerminal(term({ id: 't1', pid: 0 }), [sess({ id: 's1', pid: 0 })])).toBeUndefined()
  })

  it('sem atalho por card: pid diferente não casa mesmo com o mesmo card', () => {
    const t = term({ id: 't1', pid: 999, card_key: 'NAC-5' })
    expect(sessionOfTerminal(t, [sess({ id: 's1', pid: 200 })])).toBeUndefined()
  })

  it('matchSessions devolve só os terminais casados', () => {
    const s = sess({ id: 's1', pid: 100 })
    const m = matchSessions([term({ id: 't1', pid: 100 }), term({ id: 't2', pid: 300 })], [s])
    expect([...m.keys()]).toEqual(['t1'])
    expect(m.get('t1')).toBe(s)
  })

  it('terminalOfSession devolve o terminal de mesmo pid; pid 0 devolve undefined', () => {
    const a = term({ id: 'a', pid: 100 })
    const b = term({ id: 'b', pid: 300 })
    expect(terminalOfSession({ pid: 100 }, [a, b])).toBe(a)
    expect(terminalOfSession({ pid: 0 }, [a, b, term({ id: 'z', pid: 0 })])).toBeUndefined()
  })
})
