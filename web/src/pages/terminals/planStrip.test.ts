import { claudeChip, claudeWindows, kiroText, planWindow, worstTone } from './planStrip'
import type { AgentSpend, PlanLimits, PlanWindowState } from '../../api/types'

const now = new Date(2026, 10, 26, 15, 16).getTime()
const win = (o: Partial<PlanWindowState> = {}): PlanWindowState => ({
  used_pct: 62, resets_at: new Date(2026, 10, 26, 17, 30).toISOString(), received_at: new Date(2026, 10, 26, 15, 0).toISOString(), expired: false, ...o,
})

describe('planWindow', () => {
  it('normal', () => {
    const v = planWindow('five_hour', win(), 80, now)!
    expect(v).toMatchObject({ pct: 62, value: '62%', tone: 'ok', reset: 'reinicia em 2 h 14 min (17:30)', label: 'Sessão de 5 h', short: '5 h' })
    const s = planWindow('seven_day', win({ used_pct: 41, resets_at: new Date(2026, 10, 28, 9, 0).toISOString() }), 80, new Date(2026, 10, 26, 6, 0).getTime())!
    expect(s.reset).toBe('reinicia em 2 d 3 h (sáb 09:00)')
    expect(s.label).toBe('Semana')
  })
  it('tons', () => {
    expect(planWindow('five_hour', win({ used_pct: 85 }), 80, now)!.tone).toBe('warn')
    expect(planWindow('five_hour', win({ used_pct: 90 }), 80, now)!.tone).toBe('crit')
    expect(planWindow('five_hour', win({ used_pct: 93 }), 95, now)!.tone).toBe('crit')
  })
  it('vencida', () => {
    const stale = { pct: null, value: 'sem leitura recente', reset: '', tone: 'stale' }
    expect(planWindow('five_hour', win({ expired: true }), 80, now)).toMatchObject(stale)
    expect(planWindow('five_hour', win({ resets_at: new Date(now - 60000).toISOString() }), 80, now)).toMatchObject(stale)
  })
  it('sem horário de reinício', () => {
    expect(planWindow('five_hour', win({ resets_at: null }), 80, now)!.reset).toBe('sem horário de reinício')
  })
  it('sem dado', () => {
    expect(planWindow('five_hour', null, 80, now)).toBeNull()
    expect(claudeWindows(undefined, now)).toEqual([])
    expect(claudeWindows({ five_hour: null, seven_day: null, history: { five_hour: null, seven_day: null }, warn_pct: 80 }, now)).toEqual([])
    expect(claudeWindows([] as never, now)).toEqual([])
  })
})

describe('claudeChip e worstTone', () => {
  const p = (o: Partial<PlanLimits> = {}): PlanLimits => ({
    five_hour: win(), seven_day: win({ used_pct: 41, resets_at: new Date(2026, 10, 28, 9, 0).toISOString() }),
    history: { five_hour: null, seven_day: null }, warn_pct: 80, ...o,
  })
  it('chip', () => {
    expect(claudeChip(claudeWindows(p(), now))).toBe('Claude 5 h 62% · sem 41%')
    expect(claudeChip(claudeWindows(p({ five_hour: win({ expired: true }) }), now))).toBe('Claude 5 h sem leitura recente · sem 41%')
  })
  it('pior tom: crit > warn > stale > ok', () => {
    const tones = (a: number, b: number) => worstTone(claudeWindows(p({ five_hour: win({ used_pct: a }), seven_day: win({ used_pct: b }) }), now))
    expect(tones(10, 20)).toBe('ok')
    expect(tones(10, 85)).toBe('warn')
    expect(tones(95, 85)).toBe('crit')
    expect(worstTone(claudeWindows(p({ five_hour: win({ expired: true }) }), now))).toBe('stale')
    expect(worstTone(claudeWindows(p({ five_hour: win({ expired: true }), seven_day: win({ used_pct: 85 }) }), now))).toBe('warn')
  })
})

describe('kiroText', () => {
  const k = (credits: number): AgentSpend[] => [{ agent: 'kiro', credits, spent_usd: 0, daily_usd: 0, pct: 0, warn_pct: 80, action: '', unpriced: false }]
  it('créditos', () => {
    expect(kiroText(k(12.34), true)).toBe('12,3 créditos hoje')
    expect(kiroText(k(12.34), false)).toBeNull()
    expect(kiroText(k(0), true)).toBeNull()
    expect(kiroText(undefined, true)).toBeNull()
  })
})
