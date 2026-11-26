import { describe, expect, it } from 'vitest'
import { intervalMs, percentile, tick, verdict } from './load'

const text = (b: Uint8Array) => new TextDecoder().decode(b)
const count = (s: string, sub: string) => s.split(sub).length - 1

describe('tick', () => {
  it('agente redesenha 8 linhas (sobe 8 linhas antes)', () => {
    const s = text(tick('agente', 1))
    expect(s).toContain('\x1b[8A')
    expect(count(s, '\n')).toBe(8)
  })

  it('agente solta 30 linhas a mais a cada 25 ticks', () => {
    expect(count(text(tick('agente', 25)), '\n')).toBe(8 + 30)
    expect(count(text(tick('agente', 26)), '\n')).toBe(8)
    expect(count(text(tick('agente', 50)), '\n')).toBe(8 + 30)
  })

  it('enxurrada emite 20 linhas de 120 colunas', () => {
    const s = text(tick('enxurrada', 1))
    expect(count(s, '\n')).toBe(20)
    for (const l of s.split('\r\n').filter(Boolean)) expect(l.length).toBe(120)
  })

  it('é determinístico', () => {
    expect(tick('enxurrada', 7)).toEqual(tick('enxurrada', 7))
  })
})

describe('intervalMs', () => {
  it('agente 80 ms, enxurrada 10 ms', () => {
    expect(intervalMs('agente')).toBe(80)
    expect(intervalMs('enxurrada')).toBe(10)
  })
})

describe('percentile', () => {
  it('p95 de 1..100 é 95; vazio é 0', () => {
    expect(percentile(Array.from({ length: 100 }, (_, i) => i + 1), 95)).toBe(95)
    expect(percentile([], 95)).toBe(0)
  })
  it('não depende da ordem de entrada', () => {
    expect(percentile([5, 1, 3], 100)).toBe(5)
  })
})

describe('verdict', () => {
  it('aprova dentro da meta', () => {
    expect(verdict({ longMax: 0, lagP95: 10, lagMax: 40 })).toBe('aprovado')
  })
  it('reprova se qualquer limite estoura (limites exclusivos)', () => {
    expect(verdict({ longMax: 60, lagP95: 10, lagMax: 40 })).toBe('reprovado')
    expect(verdict({ longMax: 0, lagP95: 50, lagMax: 40 })).toBe('reprovado')
    expect(verdict({ longMax: 0, lagP95: 10, lagMax: 100 })).toBe('reprovado')
    expect(verdict({ longMax: 49, lagP95: 49, lagMax: 99 })).toBe('aprovado')
  })
})
