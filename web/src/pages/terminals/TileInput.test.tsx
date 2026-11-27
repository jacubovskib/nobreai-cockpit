import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { TileInput, type TileInputProps } from './TileInput'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Prompt } from '../../api/types'
import { inputFor } from './inputRegistry'
import { draftFor, resetDrafts } from './inputDrafts'

const URL = '/api/terminals/t1/input'

function mock(status = 200, body: unknown = { ok: true }) {
  const fn = vi.fn(async (_url: string, _init?: { method?: string; body?: string }) => new Response(JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fn)
  return fn
}
const bodies = (fn: { mock: { calls: unknown[][] } }) => (fn.mock.calls as [string, { body?: string }?][]).filter(([u]) => u === URL).map(([, i]) => JSON.parse(i?.body ?? '{}'))

const setup = (over: Partial<TileInputProps> = {}) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><TileInput terminalId="t1" agent="claude" agentName="Claude Code" cardKey="NAC-7" project={null} run={false} simple={false} {...over} /></QueryClientProvider>)
}
const box = () => screen.getByRole('textbox', { name: 'Enviar ao agente Claude Code' }) as HTMLTextAreaElement
const type = (v: string) => fireEvent.change(box(), { target: { value: v } })

beforeEach(() => resetDrafts())
afterEach(() => vi.unstubAllGlobals())

describe('TileInput', () => {
  it('Enter envia o texto e esvazia o campo mantendo o foco', async () => {
    const fn = mock()
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(box()).toHaveValue(''))
    expect(bodies(fn)).toEqual([{ text: 'oi', prompt: '' }])
    expect(box()).toHaveFocus()
  })

  it('o botão Enviar também envia', async () => {
    const fn = mock()
    setup()
    type('oi')
    fireEvent.click(screen.getByRole('button', { name: 'Enviar' }))
    await waitFor(() => expect(box()).toHaveValue(''))
    expect(bodies(fn)).toEqual([{ text: 'oi', prompt: '' }])
  })

  it('Shift+Enter não envia', () => {
    const fn = mock()
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter', shiftKey: true })
    expect(bodies(fn)).toEqual([])
    expect(box()).toHaveValue('oi')
  })

  it('Enter durante composição não envia', () => {
    const fn = mock()
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter', isComposing: true })
    expect(bodies(fn)).toEqual([])
  })

  it('texto só com espaços: Enviar desligado e Enter não faz nada', () => {
    const fn = mock()
    setup()
    type('   ')
    expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled()
    fireEvent.keyDown(box(), { key: 'Enter' })
    expect(bodies(fn)).toEqual([])
  })

  it('pendente: campo editável, botão e Enter desligados', async () => {
    let reply: (r: Response) => void = () => {}
    const fn = vi.fn((url: string, _init?: { body?: string }) => url === URL ? new Promise<Response>((res) => { reply = res }) : Promise.resolve(new Response('[]', { status: 200 })))
    vi.stubGlobal('fetch', fn)
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled())
    expect(box()).toBeEnabled()
    type('oi2')
    fireEvent.keyDown(box(), { key: 'Enter' })
    expect(bodies(fn)).toHaveLength(1)
    reply(new Response('{}', { status: 200 }))
    await waitFor(() => expect(box()).toHaveValue('oi2'))
    expect(box()).toHaveFocus()
  })

  it('o texto digitado durante o envio é mantido quando o envio termina', async () => {
    let reply: (r: Response) => void = () => {}
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((res) => { reply = res })))
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled())
    type('novo')
    await act(async () => { reply(new Response('{}', { status: 200 })) })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Enviar' })).toBeEnabled())
    expect(box()).toHaveValue('novo')
  })

  it('409 mantém o texto e mostra Terminal encerrado.; some na próxima edição', async () => {
    mock(409, { error: 'x' })
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Terminal encerrado.'))
    expect(box()).toHaveValue('oi')
    type('oi!')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('404 mostra a mensagem do daemon antigo', async () => {
    mock(404, {})
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Este daemon não aceita entrada pela barra. Atualize o Cockpit.'))
    expect(box()).toHaveValue('oi')
  })

  it('o campo cresce com as quebras de linha, de 1 a 6', () => {
    setup()
    expect(box().rows).toBe(1)
    type('a\nb\nc')
    expect(box().rows).toBe(3)
    type('1\n2\n3\n4\n5\n6\n7\n8')
    expect(box().rows).toBe(6)
  })

  it('run mostra Liberar entrada; clicar mostra a barra e registra', () => {
    setup({ run: true })
    expect(screen.getByText('Sessão do nobre run: você só assiste.')).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(inputFor('t1')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Liberar entrada' }))
    expect(box()).toBeInTheDocument()
    expect(inputFor('t1')).not.toBeNull()
  })

  it('simple esconde a linha de dicas', () => {
    const a = setup()
    expect(screen.getByText('/ @ vão direto ao agente · # prompts · Ctrl K buscar')).toBeInTheDocument()
    a.unmount()
    setup({ simple: true })
    expect(screen.queryByText(/vão direto ao agente/)).toBeNull()
  })

  it('insert com o cursor no meio de x|y dá xabcy', () => {
    setup()
    type('xy')
    box().setSelectionRange(1, 1)
    fireEvent.select(box())
    act(() => inputFor('t1')!.insert('abc'))
    expect(box()).toHaveValue('xabcy')
    expect(box().selectionStart).toBe(4)
  })

  it('desmontar tira o registro', () => {
    const a = setup()
    expect(inputFor('t1')).not.toBeNull()
    a.unmount()
    expect(inputFor('t1')).toBeNull()
  })

  it('desmontar e montar de novo (pausa) devolve o texto e o Liberar entrada', () => {
    const a = setup({ run: true })
    fireEvent.click(screen.getByRole('button', { name: 'Liberar entrada' }))
    type('rascunho')
    a.unmount()
    expect(draftFor('t1')).toEqual({ text: 'rascunho', freed: true })
    setup({ run: true })
    expect(box()).toHaveValue('rascunho')
  })

  it('enviar esvazia o rascunho guardado', async () => {
    mock()
    const a = setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(box()).toHaveValue(''))
    a.unmount()
    setup()
    expect(box()).toHaveValue('')
  })

  it('envio que termina com a barra desmontada não deixa o texto enviado no rascunho', async () => {
    let reply: (r: Response) => void = () => {}
    vi.stubGlobal('fetch', vi.fn((url: string) => url === URL ? new Promise<Response>((res) => { reply = res }) : Promise.resolve(new Response('[]', { status: 200 }))))
    const a = setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled())
    a.unmount()
    await act(async () => { reply(new Response('{}', { status: 200 })) })
    expect(draftFor('t1').text).toBe('')
  })

  it('o rodapé reserva 30px à direita para o canto de redimensionar', () => {
    setup()
    expect((screen.getByTestId('tile-input') as HTMLElement).style.paddingRight).toBe('30px')
  })

  describe('# prompts salvos', () => {
    const pr = (o: Partial<Prompt>): Prompt => ({ id: 1, name: 'Revisar', slug: 'revisar', tag: '', description: '', body: 'Revise {{card}}\nlinha 2', agents: ['claude'], project_id: null, is_default_handoff: false, uses: 0, last_used_at: null, ...o })
    const PROMPTS = [
      pr({ id: 1, name: 'Alfa', slug: 'alfa', description: 'Primeira linha\nsegunda', uses: 5 }),
      pr({ id: 2, name: 'Revisar', slug: 'revisar', uses: 1 }),
      pr({ id: 3, name: 'Outro agente', slug: 'outro', agents: ['codex'] }),
    ]
    function mockApi(o: { render?: (url: string, body: { card: string }) => Response; send?: number } = {}) {
      const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
        if (url === '/api/prompts') return new Response(JSON.stringify(PROMPTS), { status: 200 })
        if (url.startsWith('/api/prompts/') && url.endsWith('/render')) {
          const body = JSON.parse(init?.body ?? '{}')
          return o.render ? o.render(url, body) : new Response(JSON.stringify({ text: 'Texto de ' + url.split('/')[3] + ' ' + body.card }), { status: 200 })
        }
        return new Response(JSON.stringify({ ok: true }), { status: o.send ?? 200 })
      })
      vi.stubGlobal('fetch', fn)
      return fn
    }
    const renders = (fn: ReturnType<typeof mockApi>) => fn.mock.calls.filter(([u]) => u.endsWith('/render')).map(([u, i]) => [u, JSON.parse(i?.body ?? '{}')])
    const list = () => screen.queryByRole('listbox', { name: 'Prompts salvos' })
    const opts = () => screen.getAllByRole('option')

    it('# abre a lista só com os prompts do agente, com nome e primeira linha', async () => {
      mockApi()
      setup()
      expect(list()).toBeNull()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      expect(opts()[0]).toHaveTextContent('Alfa')
      expect(opts()[0]).toHaveTextContent('Primeira linha')
      expect(opts()[0]).not.toHaveTextContent('segunda')
      expect(opts()[1]).toHaveTextContent('Revise {{card}}')
      expect(opts()[0]).toHaveAttribute('aria-selected', 'true')
    })

    it('#rev filtra; sem resultado mostra a mensagem', async () => {
      mockApi()
      setup()
      type('#rev')
      await waitFor(() => expect(opts()).toHaveLength(1))
      type('#zzz')
      expect(screen.getByText('Nenhum prompt com “zzz”.')).toBeInTheDocument()
    })

    it('setas movem a seleção e Enter escolhe o segundo', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.keyDown(box(), { key: 'ArrowDown' })
      expect(opts()[1]).toHaveAttribute('aria-selected', 'true')
      fireEvent.keyDown(box(), { key: 'ArrowUp' })
      expect(opts()[0]).toHaveAttribute('aria-selected', 'true')
      fireEvent.keyDown(box(), { key: 'ArrowDown' })
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(box()).toHaveValue('Texto de revisar NAC-7'))
      expect(renders(fn)).toEqual([['/api/prompts/revisar/render', { card: 'NAC-7' }]])
      expect(list()).toBeNull()
      expect(screen.getByText('Prévia: o agente vai receber este texto. Edite antes de enviar.')).toBeInTheDocument()
    })

    it('clicar numa opção escolhe', async () => {
      mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
    })

    it('Esc fecha a lista e mantém o texto; Enter depois envia #abc', async () => {
      const fn = mockApi()
      setup()
      type('#abc')
      await waitFor(() => expect(screen.getByText('Nenhum prompt com “abc”.')).toBeInTheDocument())
      fireEvent.keyDown(box(), { key: 'Escape' })
      expect(list()).toBeNull()
      expect(box()).toHaveValue('#abc')
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: '#abc', prompt: '' }]))
    })

    it('a lista reabre quando o texto deixa de começar com # e volta a começar', async () => {
      mockApi()
      setup()
      type('#a')
      await waitFor(() => expect(list()).not.toBeNull())
      fireEvent.keyDown(box(), { key: 'Escape' })
      type('a')
      type('#')
      await waitFor(() => expect(list()).not.toBeNull())
    })

    it('Enter com lista vazia não envia', async () => {
      const fn = mockApi()
      setup()
      type('#zzz')
      await waitFor(() => expect(screen.getByText('Nenhum prompt com “zzz”.')).toBeInTheDocument())
      fireEvent.keyDown(box(), { key: 'Enter' })
      expect(bodies(fn)).toEqual([])
      expect(box()).toHaveValue('#zzz')
    })

    it('422 não muda a barra e lista as variáveis desconhecidas', async () => {
      mockApi({ render: () => new Response(JSON.stringify({ unknown: ['a', 'b'] }), { status: 422 }) })
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Variável desconhecida: {{a}}, {{b}}'))
      expect(box()).toHaveValue('#')
      expect(list()).not.toBeNull()
    })

    it('outro erro mostra Não foi possível carregar o prompt.', async () => {
      mockApi({ render: () => new Response('{}', { status: 500 }) })
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Não foi possível carregar o prompt.'))
    })

    it('tile sem card renderiza com card vazio e avisa', async () => {
      const fn = mockApi()
      setup({ cardKey: '' })
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(screen.getByText('Sem card: as variáveis do card ficaram vazias.')).toBeInTheDocument())
      expect(renders(fn)).toEqual([['/api/prompts/alfa/render', { card: '' }]])
    })

    it('404 com card repete sem card e avisa', async () => {
      const fn = mockApi({ render: (_u, b) => b.card ? new Response('{}', { status: 404 }) : new Response(JSON.stringify({ text: 'ok' }), { status: 200 }) })
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('ok'))
      expect(renders(fn).map((r) => r[1])).toEqual([{ card: 'NAC-7' }, { card: '' }])
      expect(screen.getByText('Sem card: as variáveis do card ficaram vazias.')).toBeInTheDocument()
    })

    it('resultado que começa com # avisa do comando e não reabre a lista', async () => {
      mockApi({ render: () => new Response(JSON.stringify({ text: '#resumo' }), { status: 200 }) })
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('#resumo'))
      expect(screen.getByText('O agente pode tratar o primeiro caractere como comando.')).toBeInTheDocument()
      expect(list()).toBeNull()
    })

    it('enviar depois de escolher manda prompt: slug e esquece o slug', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[1])
      await waitFor(() => expect(box()).toHaveValue('Texto de revisar NAC-7'))
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(box()).toHaveValue(''))
      expect(bodies(fn)).toEqual([{ text: 'Texto de revisar NAC-7', prompt: 'revisar' }])
      expect(screen.queryByText(/^Prévia:/)).toBeNull()
      type('de novo')
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toHaveLength(2))
      expect(bodies(fn)[1]).toEqual({ text: 'de novo', prompt: '' })
    })

    it('escolher, apagar tudo, digitar x e enviar manda prompt vazio', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      type('')
      type('x')
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'x', prompt: '' }]))
    })

    it('escolher, trocar o texto todo por x (selecionar tudo e digitar) e enviar manda prompt vazio', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      type('x')
      expect(screen.queryByText(/^Prévia:/)).toBeNull()
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'x', prompt: '' }]))
    })

    it('escolher, trocar o texto todo por # reabre a lista', async () => {
      mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      type('#')
      await waitFor(() => expect(list()).not.toBeNull())
    })

    it('escolher e acrescentar texto mantém o slug no envio', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      type('Texto de alfa NAC-7 e mais isto')
      expect(screen.getByText(/^Prévia:/)).toBeInTheDocument()
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'Texto de alfa NAC-7 e mais isto', prompt: 'alfa' }]))
    })

    it('escolher, selecionar tudo e inserir um card pela paleta esquece o slug', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      box().setSelectionRange(0, box().value.length)
      act(() => inputFor('t1')!.insert('NAC-9 · Outro'))
      expect(box()).toHaveValue('NAC-9 · Outro')
      expect(screen.queryByText(/^Prévia:/)).toBeNull()
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'NAC-9 · Outro', prompt: '' }]))
    })

    it('escolher e inserir um card no fim pela paleta mantém o slug', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      act(() => inputFor('t1')!.insert(' NAC-9'))
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'Texto de alfa NAC-7 NAC-9', prompt: 'alfa' }]))
    })

    it('a lista abre por cima da barra (absoluta), sem mudar a altura do terminal', async () => {
      mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      const lb = list() as HTMLElement
      expect(lb.style.position).toBe('absolute')
      expect(lb.style.bottom).toBe('100%')
      expect((screen.getByTestId('tile-input') as HTMLElement).style.position).toBe('relative')
    })

    it('sem resultado, o aviso também fica por cima da barra', async () => {
      mockApi()
      setup()
      type('#zzz')
      const msg = await screen.findByText('Nenhum prompt com “zzz”.')
      expect((msg as HTMLElement).style.position).toBe('absolute')
    })

    it('Limpar esvazia a barra e esquece o slug', async () => {
      const fn = mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
      expect(box()).toHaveValue('')
      expect(screen.queryByText(/^Prévia:/)).toBeNull()
      type('y')
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'y', prompt: '' }]))
    })

    it('depois de enviar o prompt escolhido, # abre a lista de novo', async () => {
      mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(box()).toHaveValue(''))
      type('#')
      await waitFor(() => expect(list()).not.toBeNull())
    })

    it('depois de Limpar, # abre a lista de novo', async () => {
      mockApi()
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
      type('#')
      await waitFor(() => expect(list()).not.toBeNull())
    })

    it('resposta atrasada do render não sobrescreve o que foi digitado', async () => {
      let reply: (r: Response) => void = () => {}
      const fn = vi.fn((url: string, _init?: { body?: string }) => {
        if (url === '/api/prompts') return Promise.resolve(new Response(JSON.stringify(PROMPTS), { status: 200 }))
        if (url.endsWith('/render')) return new Promise<Response>((res) => { reply = res })
        return Promise.resolve(new Response('{}', { status: 200 }))
      })
      vi.stubGlobal('fetch', fn)
      setup()
      type('#')
      await waitFor(() => expect(opts()).toHaveLength(2))
      fireEvent.click(opts()[0])
      type('outra coisa')
      await act(async () => { reply(new Response(JSON.stringify({ text: 'tardio' }), { status: 200 })) })
      expect(box()).toHaveValue('outra coisa')
      expect(screen.queryByText(/^Prévia:/)).toBeNull()
      fireEvent.keyDown(box(), { key: 'Enter' })
      await waitFor(() => expect(bodies(fn)).toEqual([{ text: 'outra coisa', prompt: '' }]))
    })

    it('pickPrompt do registro tem o mesmo efeito do clique', async () => {
      const fn = mockApi()
      setup()
      await act(async () => { inputFor('t1')!.pickPrompt('alfa') })
      await waitFor(() => expect(box()).toHaveValue('Texto de alfa NAC-7'))
      expect(renders(fn)).toEqual([['/api/prompts/alfa/render', { card: 'NAC-7' }]])
      expect(box()).toHaveFocus()
      expect(screen.getByText(/^Prévia:/)).toBeInTheDocument()
    })
  })
})
