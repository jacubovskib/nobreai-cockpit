import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { Card, Memory, Project, Prompt } from '../../api/types'

const post = vi.fn()
vi.mock('../../api/client', async (orig) => ({ ...(await orig<typeof import('../../api/client')>()), post: (...a: unknown[]) => post(...a) }))

import {
  MAX_GROUP, cardPiece, commandWarning, inputError, insertAt, memoryPiece, paletteCards, paletteMemories, promptsFor, replacedAll, sendInput,
} from './inputModel'

const prompt = (o: Partial<Prompt>) => ({ name: 'P', slug: 'p', tag: '', agents: ['claude'], project_id: null, uses: 0, ...o }) as Prompt
const proj = (o: Partial<Project>) => ({ id: 1, parent_id: null, ...o }) as Project
const card = (o: Partial<Card>) => ({ key: 'NAC-1', title: 'T', where_stopped: '', column: 'fazendo', updated_at: '2026-01-01', ...o }) as Card
const mem = (o: Partial<Memory>) => ({ rule: 'r', do: '', status: 'ativa', project_id: null, ...o }) as Memory

describe('promptsFor', () => {
  it('só entra o prompt cujo agents inclui o agente', () => {
    const ps = [prompt({ slug: 'a' }), prompt({ slug: 'b', agents: ['codex'] })]
    expect(promptsFor(ps, 'claude', null, '').map((p) => p.slug)).toEqual(['a'])
  })
  it('projeto filho herda os do pai e ignora os de outros projetos', () => {
    const ps = [prompt({ slug: 'g' }), prompt({ slug: 'pai', project_id: 1 }), prompt({ slug: 'eu', project_id: 2 }), prompt({ slug: 'outro', project_id: 3 })]
    const got = promptsFor(ps, 'claude', proj({ id: 2, parent_id: 1 }), '').map((p) => p.slug).sort()
    expect(got).toEqual(['eu', 'g', 'pai'])
  })
  it('sem projeto só entram os globais', () => {
    const ps = [prompt({ slug: 'g' }), prompt({ slug: 'x', project_id: 1 })]
    expect(promptsFor(ps, 'claude', null, '').map((p) => p.slug)).toEqual(['g'])
  })
  it('busca sem diferenciar maiúsculas nem acentos em name, slug ou tag', () => {
    const ps = [prompt({ name: 'Revisão', slug: 'x' }), prompt({ name: 'N', slug: 'rev-2' }), prompt({ name: 'M', slug: 'm', tag: 'REVisar' }), prompt({ name: 'Outro', slug: 'o' })]
    expect(promptsFor(ps, 'claude', null, 'revi').map((p) => p.name).sort()).toEqual(['M', 'Revisão'])
    expect(promptsFor(ps, 'claude', null, 'REV-').map((p) => p.name)).toEqual(['N'])
  })
  it('ordena por uses decrescente e empata por name, sem corte', () => {
    const ps = Array.from({ length: 12 }, (_, i) => prompt({ name: `n${String(i).padStart(2, '0')}`, uses: i < 2 ? 5 : 0 }))
    const got = promptsFor([...ps].reverse(), 'claude', null, '')
    expect(got).toHaveLength(12)
    expect(got.slice(0, 3).map((p) => p.name)).toEqual(['n00', 'n01', 'n02'])
  })
})

describe('insertAt', () => {
  it('troca o trecho e põe o cursor depois da peça', () => {
    expect(insertAt('abcdef', 2, 4, 'XYZ')).toEqual({ text: 'abXYZef', caret: 5 })
    expect(insertAt('', 0, 0, 'oi')).toEqual({ text: 'oi', caret: 2 })
  })
})

describe('cardPiece e memoryPiece', () => {
  it('cardPiece com e sem onde parei', () => {
    expect(cardPiece(card({ key: 'NAC-12', title: 'Título' }))).toBe('NAC-12 · Título')
    expect(cardPiece(card({ key: 'NAC-12', title: 'Título', where_stopped: 'no teste' }))).toBe('NAC-12 · Título\nOnde parei: no teste')
  })
  it('memoryPiece com e sem do', () => {
    expect(memoryPiece(mem({ rule: 'Regra' }))).toBe('Regra')
    expect(memoryPiece(mem({ rule: 'Regra', do: 'faça' }))).toBe('Regra — faça')
  })
})

describe('paletteCards', () => {
  it('exclui concluídos, ordena por updated_at e busca em key ou title', () => {
    const cs = [
      card({ key: 'A-1', title: 'velho', updated_at: '2026-01-01' }),
      card({ key: 'A-2', title: 'novo', updated_at: '2026-03-01' }),
      card({ key: 'A-3', title: 'feito', column: 'concluido', updated_at: '2026-04-01' }),
    ]
    expect(paletteCards(cs, '').map((c) => c.key)).toEqual(['A-2', 'A-1'])
    expect(paletteCards(cs, 'a-1').map((c) => c.key)).toEqual(['A-1'])
    expect(paletteCards(cs, 'NOV').map((c) => c.key)).toEqual(['A-2'])
    expect(paletteCards(cs, 'zzz')).toEqual([])
  })
  it('limita a MAX_GROUP', () => {
    const cs = Array.from({ length: 20 }, (_, i) => card({ key: `K-${i}` }))
    expect(paletteCards(cs, '')).toHaveLength(MAX_GROUP)
  })
})

describe('paletteMemories', () => {
  it('só ativas e, sem projeto, só as globais', () => {
    const ms = [mem({ rule: 'a' }), mem({ rule: 'b', status: 'candidata' }), mem({ rule: 'c', project_id: 3 })]
    expect(paletteMemories(ms, null, '').map((m) => m.rule)).toEqual(['a'])
    expect(paletteMemories(ms, proj({ id: 3 }), '').map((m) => m.rule)).toEqual(['a', 'c'])
  })
  it('busca em rule ou do e limita a MAX_GROUP', () => {
    const ms = [mem({ rule: 'usar x', do: 'ok' }), mem({ rule: 'y', do: 'Ação' })]
    expect(paletteMemories(ms, null, 'acao').map((m) => m.rule)).toEqual(['y'])
    expect(paletteMemories(ms, null, 'usar').map((m) => m.rule)).toEqual(['usar x'])
    expect(paletteMemories(Array.from({ length: 20 }, () => mem({})), null, '')).toHaveLength(MAX_GROUP)
  })
})

describe('commandWarning', () => {
  it('avisa para # / !', () => {
    for (const t of ['#a', '/clear', '!ls']) expect(commandWarning(t)).toBe(true)
  })
  it('não avisa para texto comum ou vazio', () => {
    expect(commandWarning('oi # x')).toBe(false)
    expect(commandWarning(' /x')).toBe(false)
    expect(commandWarning('')).toBe(false)
  })
})

describe('sendInput', () => {
  beforeEach(() => post.mockReset())
  it('posta text e prompt (vazio por padrão)', async () => {
    post.mockResolvedValue(null)
    await sendInput('t1', 'oi')
    expect(post).toHaveBeenCalledWith('/api/terminals/t1/input', { text: 'oi', prompt: '' })
    await sendInput('t1', 'oi', 'rev')
    expect(post).toHaveBeenLastCalledWith('/api/terminals/t1/input', { text: 'oi', prompt: 'rev' })
  })
})

describe('inputError', () => {
  it('traduz o status', () => {
    const e = (s: number) => inputError(new ApiError(s, 'x'))
    expect(e(404)).toBe('Este daemon não aceita entrada pela barra. Atualize o Cockpit.')
    expect(e(405)).toBe('Este daemon não aceita entrada pela barra. Atualize o Cockpit.')
    expect(e(409)).toBe('Terminal encerrado.')
    expect(e(413)).toBe('Texto grande demais para enviar.')
    expect(e(504)).toBe('O agente não leu a entrada. Tente de novo.')
    expect(e(500)).toBe('Não foi possível enviar.')
    expect(inputError(new Error('x'))).toBe('Não foi possível enviar.')
  })
})

describe('replacedAll', () => {
  it('sem começo nem fim em comum: trocou tudo', () => {
    expect(replacedAll('Texto do prompt', 'x')).toBe(true)
    expect(replacedAll('abc', 'xyz')).toBe(true)
  })
  it('começo ou fim em comum: edição parcial', () => {
    expect(replacedAll('Texto do prompt', 'Texto do prompt e mais')).toBe(false) // acrescentou no fim
    expect(replacedAll('Texto do prompt', 'Antes. Texto do prompt')).toBe(false) // acrescentou no começo
    expect(replacedAll('Texto do prompt', 'Texto prompt')).toBe(false) // apagou no meio
    expect(replacedAll('abc', 'axc')).toBe(false)
  })
  it('esvaziar conta como trocar tudo; texto igual não', () => {
    expect(replacedAll('abc', '')).toBe(true)
    expect(replacedAll('', 'x')).toBe(true)
    expect(replacedAll('abc', 'abc')).toBe(false)
    expect(replacedAll('', '')).toBe(false)
  })
})
