import { describe, expect, it } from 'vitest'
import { defaultLayout, type Layout } from './gridModel'
import { KEY, loadLayout, saveLayout } from './gridStore'

// Armazenamento falso em memória.
function fake(initial?: string) {
  const m = new Map<string, string>()
  if (initial !== undefined) m.set(KEY, initial)
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
  }
}

describe('gridStore', () => {
  it('usa a chave combinada', () => {
    expect(KEY).toBe('nobre.terminais.grade.v1')
  })

  it('ida e volta devolve o mesmo layout', () => {
    const l: Layout = { order: ['b', 'a'], sizes: { a: { span: 2, h: 500 } }, cols: 2, simple: true, tab: 4, sub: 'geral' }
    const s = fake()
    saveLayout(l, s)
    expect(loadLayout(s)).toEqual(l)
  })

  it('sem nada salvo devolve o padrão', () => {
    expect(loadLayout(fake())).toEqual(defaultLayout())
  })

  it('getItem que lança devolve o padrão', () => {
    const s = { getItem: () => { throw new Error('bloqueado') } }
    expect(loadLayout(s)).toEqual(defaultLayout())
  })

  it('JSON inválido devolve o padrão', () => {
    expect(loadLayout(fake('{quebrado'))).toEqual(defaultLayout())
    expect(loadLayout(fake('null'))).toEqual(defaultLayout())
    expect(loadLayout(fake('[1]'))).toEqual(defaultLayout())
  })

  it('campos com tipo errado voltam ao padrão, h passa por clampH', () => {
    const raw = JSON.stringify({ cols: 7, order: 'x', sizes: { a: { span: 'z', h: -5 } } })
    const l = loadLayout(fake(raw))
    expect(l.cols).toBe(3)
    expect(l.order).toEqual([])
    expect(l.sizes).toEqual({ a: { span: 1, h: 200 } })
  })

  it('valida span como inteiro positivo e limita h', () => {
    const raw = JSON.stringify({ sizes: { a: { span: 0, h: 5000 }, b: { span: 1.5, h: 300 }, c: 'x', d: { span: 2, h: 'y' } } })
    const l = loadLayout(fake(raw))
    expect(l.sizes.a).toEqual({ span: 1, h: 900 })
    expect(l.sizes.b).toEqual({ span: 1, h: 300 })
    expect(l.sizes.c).toBeUndefined()
    expect(l.sizes.d).toEqual({ span: 2, h: 360 })
  })

  it('filtra itens de order que não são texto e valida tab, sub e simple', () => {
    const raw = JSON.stringify({ order: ['a', 3, 'b'], simple: 'sim', tab: 'abc', sub: {} })
    const l = loadLayout(fake(raw))
    expect(l.order).toEqual(['a', 'b'])
    expect(l.simple).toBe(false)
    expect(l.tab).toBe('all')
    expect(l.sub).toBe('all')
  })

  it('aceita tab e sub válidos', () => {
    const l = loadLayout(fake(JSON.stringify({ tab: 'all', sub: 'geral', cols: 1 })))
    expect(l.sub).toBe('geral')
    expect(l.cols).toBe(1)
  })

  it('nunca devolve referência compartilhada do padrão', () => {
    const a = loadLayout(fake()), b = loadLayout(fake())
    a.order.push('x')
    a.sizes.x = { span: 1, h: 300 }
    expect(b.order).toEqual([])
    expect(loadLayout(fake()).sizes).toEqual({})
  })

  it('setItem que lança não propaga o erro', () => {
    const s = { setItem: () => { throw new Error('cheio') } }
    expect(() => saveLayout(defaultLayout(), s)).not.toThrow()
  })
})
