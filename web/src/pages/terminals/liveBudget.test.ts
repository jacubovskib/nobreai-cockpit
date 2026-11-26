import { describe, expect, it } from 'vitest'
import { HIDDEN_PAUSE_MS, MAX_LIVE, liveSet, type BudgetInput } from './liveBudget'

const nomes = (n: number) => Array.from({ length: n }, (_, i) => `t${i + 1}`)

function entrada(parcial: Partial<BudgetInput> = {}): BudgetInput {
  const ids = parcial.ids ?? nomes(3)
  return {
    ids,
    onScreen: new Set(ids),
    ended: new Set(),
    woken: new Set(),
    focused: null,
    lastUsed: {},
    pageHidden: false,
    ...parcial,
  }
}

describe('liveSet', () => {
  it('expõe as constantes do orçamento', () => {
    expect(MAX_LIVE).toBe(9)
    expect(HIDDEN_PAUSE_MS).toBe(30_000)
  })

  it('não liga o que está fora da tela', () => {
    const r = liveSet(entrada({ onScreen: new Set(['t1', 't3']) }))
    expect([...r]).toEqual(['t1', 't3'])
  })

  it('página escondida desliga tudo, inclusive o focado', () => {
    const r = liveSet(entrada({ pageHidden: true, focused: 't1' }))
    expect(r.size).toBe(0)
  })

  it('encerrado só liga se estiver em woken', () => {
    const base = { ended: new Set(['t2']) }
    expect([...liveSet(entrada(base))]).toEqual(['t1', 't3'])
    expect([...liveSet(entrada({ ...base, woken: new Set(['t2']) }))].sort()).toEqual(['t1', 't2', 't3'])
  })

  it('com 12 na tela liga 9, os de maior lastUsed', () => {
    const ids = nomes(12)
    const lastUsed: Record<string, number> = {}
    ids.forEach((id, i) => (lastUsed[id] = i + 1)) // t12 é o mais recente
    const r = liveSet(entrada({ ids, lastUsed }))
    expect(r.size).toBe(9)
    expect([...r].sort()).toEqual(ids.slice(3).sort())
  })

  it('empate de lastUsed desempata pela ordem da grade', () => {
    const r = liveSet(entrada({ ids: nomes(12) }))
    expect([...r].sort()).toEqual(nomes(9).sort())
  })

  it('no empate de lastUsed, rodando vem antes de encerrado (depois, ordem da grade)', () => {
    // t1 encerrou com o tile ligado (woken) e nunca foi usado: não tira a vaga de t10.
    const ids = nomes(10)
    const r = liveSet(entrada({ ids, ended: new Set(['t1']), woken: new Set(['t1']) }))
    expect(r.size).toBe(9)
    expect(r.has('t1')).toBe(false)
    expect(r.has('t10')).toBe(true)
  })

  it('encerrado usado mais recentemente ainda vence rodando usado antes', () => {
    const ids = nomes(10)
    const lastUsed: Record<string, number> = {}
    ids.forEach((id, i) => (lastUsed[id] = i + 1)) // t1 é o menos recente dos rodando
    lastUsed.t10 = 1000 // encerrado que o usuário clicou por último
    const r = liveSet(entrada({ ids, lastUsed, ended: new Set(['t10']), woken: new Set(['t10']) }))
    expect(r.size).toBe(9)
    expect(r.has('t10')).toBe(true)
    expect(r.has('t1')).toBe(false)
  })

  it('lastUsed ausente conta como 0', () => {
    const ids = nomes(10)
    const r = liveSet(entrada({ ids, lastUsed: { t10: 5 } }))
    expect(r.has('t10')).toBe(true)
    expect(r.has('t9')).toBe(false)
    expect(r.size).toBe(9)
  })

  it('o focado entra mesmo com o menor lastUsed e o total continua 9', () => {
    const ids = nomes(12)
    const lastUsed: Record<string, number> = {}
    ids.forEach((id, i) => (lastUsed[id] = i + 100))
    lastUsed.t1 = 1
    const r = liveSet(entrada({ ids, lastUsed, focused: 't1' }))
    expect(r.size).toBe(9)
    expect(r.has('t1')).toBe(true)
    expect(r.has('t4')).toBe(false) // 8 outros: t5..t12
    expect(r.has('t5')).toBe(true)
  })

  it('o focado entra mesmo fora da tela', () => {
    const r = liveSet(entrada({ onScreen: new Set(['t2']), focused: 't1' }))
    expect([...r].sort()).toEqual(['t1', 't2'])
  })

  it('o focado encerrado entra mesmo sem estar em woken', () => {
    const r = liveSet(entrada({ ended: new Set(['t1']), focused: 't1' }))
    expect(r.has('t1')).toBe(true)
  })

  it('id fora de ids nunca liga, nem como focado', () => {
    const r = liveSet(entrada({ onScreen: new Set(['t1', 'x']), woken: new Set(['y']), focused: 'x' }))
    expect(r.has('x')).toBe(false)
    expect(r.has('y')).toBe(false)
  })

  it('respeita um max customizado, contando o focado', () => {
    const r = liveSet(entrada({ ids: nomes(5), focused: 't5' }), 2)
    expect(r.size).toBe(2)
    expect(r.has('t5')).toBe(true)
  })
})
