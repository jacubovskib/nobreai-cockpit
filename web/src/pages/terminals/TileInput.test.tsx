import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { TileInput, type TileInputProps } from './TileInput'
import { inputFor } from './inputRegistry'

const URL = '/api/terminals/t1/input'

function mock(status = 200, body: unknown = { ok: true }) {
  const fn = vi.fn(async (_url: string, _init?: { method?: string; body?: string }) => new Response(JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fn)
  return fn
}
const bodies = (fn: ReturnType<typeof mock>) => fn.mock.calls.filter(([u]) => u === URL).map(([, i]) => JSON.parse(i?.body ?? '{}'))

const setup = (over: Partial<TileInputProps> = {}) =>
  render(<TileInput terminalId="t1" agent="claude" agentName="Claude Code" cardKey="NAC-7" project={null} run={false} simple={false} {...over} />)
const box = () => screen.getByRole('textbox', { name: 'Enviar ao agente Claude Code' }) as HTMLTextAreaElement
const type = (v: string) => fireEvent.change(box(), { target: { value: v } })

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
    expect(fn).not.toHaveBeenCalled()
    expect(box()).toHaveValue('oi')
  })

  it('Enter durante composição não envia', () => {
    const fn = mock()
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter', isComposing: true })
    expect(fn).not.toHaveBeenCalled()
  })

  it('texto só com espaços: Enviar desligado e Enter não faz nada', () => {
    const fn = mock()
    setup()
    type('   ')
    expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled()
    fireEvent.keyDown(box(), { key: 'Enter' })
    expect(fn).not.toHaveBeenCalled()
  })

  it('pendente: campo editável, botão e Enter desligados', async () => {
    let reply: (r: Response) => void = () => {}
    const fn = vi.fn(() => new Promise<Response>((res) => { reply = res }))
    vi.stubGlobal('fetch', fn)
    setup()
    type('oi')
    fireEvent.keyDown(box(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Enviar' })).toBeDisabled())
    expect(box()).toBeEnabled()
    type('oi2')
    fireEvent.keyDown(box(), { key: 'Enter' })
    expect(fn).toHaveBeenCalledOnce()
    reply(new Response('{}', { status: 200 }))
    await waitFor(() => expect(box()).toHaveValue(''))
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

  it('o rodapé reserva 30px à direita para o canto de redimensionar', () => {
    setup()
    expect((screen.getByTestId('tile-input') as HTMLElement).style.paddingRight).toBe('30px')
  })
})
