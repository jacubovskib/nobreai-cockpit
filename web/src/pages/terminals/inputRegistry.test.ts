import { describe, expect, it, vi } from 'vitest'
import { inputFor, registerInput, type TileInputHandle } from './inputRegistry'

const handle = (): TileInputHandle => ({ insert: vi.fn(), pickPrompt: vi.fn(), focus: vi.fn() })

describe('registro de entrada', () => {
  it('devolve o handle registrado e null para id desconhecido ou nulo', () => {
    const h = handle()
    const off = registerInput('a', h)
    expect(inputFor('a')).toBe(h)
    expect(inputFor('b')).toBeNull()
    expect(inputFor(null)).toBeNull()
    off()
    expect(inputFor('a')).toBeNull()
  })

  it('registrar o mesmo id de novo substitui o handle', () => {
    const h1 = handle()
    const h2 = handle()
    registerInput('s', h1)
    const off2 = registerInput('s', h2)
    expect(inputFor('s')).toBe(h2)
    off2()
  })

  it('o cancelamento velho não apaga o handle novo', () => {
    const h1 = handle()
    const h2 = handle()
    const off1 = registerInput('x', h1)
    const off2 = registerInput('x', h2)
    off1()
    expect(inputFor('x')).toBe(h2)
    off2()
    expect(inputFor('x')).toBeNull()
  })
})
