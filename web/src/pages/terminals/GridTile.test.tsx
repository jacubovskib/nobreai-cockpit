import { forwardRef, useImperativeHandle } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { TerminalInfo } from '../../api/terminals'
import type { TerminalHandle, TerminalViewProps } from '../../ui/TerminalView'
import { GridTile, type GridTileProps } from './GridTile'
import { draftFor, resetDrafts } from './inputDrafts'
import { inputFor } from './inputRegistry'

// Terminal simulado: guarda as props e o handle para o teste comandar.
const handle = { setReadonly: vi.fn(), kill: vi.fn(), fit: vi.fn(), reconnect: vi.fn(), holdFit: vi.fn(), releaseFit: vi.fn() }
let last: TerminalViewProps | null = null
vi.mock('../../ui/LazyTerminalView', () => ({
  LazyTerminalView: forwardRef<TerminalHandle, TerminalViewProps>(function Fake(props, ref) {
    last = props
    useImperativeHandle(ref, () => handle)
    return <div data-testid="fake-term" data-grid={String(!!props.grid)} data-readonly={String(!!props.readonly)} />
  }),
}))

const term = (o: Partial<TerminalInfo> = {}): TerminalInfo => ({
  id: 't1', card_key: 'NAC-7', agent: 'claude', origin: 'cockpit', title: '', pid: 1,
  started_at: new Date(Date.now() - 5 * 60000).toISOString(), ended_at: null, exit_code: null, cols: 80, rows: 24, clients: 2, ...o,
})

function setup(over: Partial<GridTileProps> = {}) {
  const props: GridTileProps = {
    t: term(), project: null, projects: [], size: { span: 1, h: 360 }, cols: 3, simple: false, live: true, focused: false,
    onFocus: vi.fn(), onWake: vi.fn(), onStep: vi.fn(), onToggleFull: vi.fn(), onResize: vi.fn(),
    onDragStart: vi.fn(), onDragOverTile: vi.fn(), onDropTile: vi.fn(), onDragEnd: vi.fn(), dropMark: null, ...over,
  }
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const ui = (p: GridTileProps) => <QueryClientProvider client={qc}><MemoryRouter><GridTile {...p} /></MemoryRouter></QueryClientProvider>
  const r = render(ui(props))
  return { props, ...r, rerenderWith: (o: Partial<GridTileProps>) => r.rerender(ui({ ...props, ...o })) }
}

beforeEach(() => { Object.values(handle).forEach((f) => f.mockClear()); last = null; resetDrafts() })

describe('GridTile', () => {
  it('em pausa mostra o botão e clicar liga', () => {
    const { props } = setup({ live: false })
    expect(screen.queryByTestId('fake-term')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Em pausa\. Clique para ligar\./ }))
    expect(props.onWake).toHaveBeenCalledOnce()
  })

  it('ligado renderiza o terminal com grid e readonly', () => {
    setup()
    const el = screen.getByTestId('fake-term')
    expect(el).toHaveAttribute('data-grid', 'true')
    expect(el).toHaveAttribute('data-readonly', 'true')
  })

  it('a raiz leva data-tile, span e altura', () => {
    const { container } = setup({ size: { span: 2, h: 420 } })
    const s = container.querySelector('section[data-tile="t1"]') as HTMLElement
    expect(s.style.gridColumn).toBe('span 2')
    expect(s.style.height).toBe('420px')
  })

  it('Simples esconde origem e clientes; Detalhado mostra', () => {
    const a = setup({ simple: true })
    expect(screen.queryByText('cockpit')).toBeNull()
    expect(screen.queryByText(/2 clientes/)).toBeNull()
    a.unmount()
    setup()
    expect(screen.getByText('cockpit')).toBeInTheDocument()
    expect(screen.getByText(/2 clientes/)).toBeInTheDocument()
    expect(screen.getByText('há 5 min')).toBeInTheDocument()
  })

  it('−, + e ⤢ chamam os callbacks, com aria-label', () => {
    const { props } = setup()
    fireEvent.click(screen.getByRole('button', { name: 'Diminuir terminal' }))
    fireEvent.click(screen.getByRole('button', { name: 'Aumentar terminal' }))
    fireEvent.click(screen.getByRole('button', { name: 'Ocupar a linha toda' }))
    expect(props.onStep).toHaveBeenNthCalledWith(1, -1)
    expect(props.onStep).toHaveBeenNthCalledWith(2, 1)
    expect(props.onToggleFull).toHaveBeenCalledOnce()
  })

  it('o card tem link', () => {
    setup()
    expect(screen.getByRole('link', { name: 'NAC-7' })).toHaveAttribute('href', expect.stringContaining('/cards/NAC-7'))
  })

  it('o menu não aparece em terminal encerrado nem com o tile em pausa', () => {
    const a = setup({ t: term({ ended_at: new Date().toISOString(), exit_code: 3 }) })
    expect(screen.queryByRole('button', { name: 'Mais ações' })).toBeNull()
    expect(screen.getByText('encerrado (código 3)')).toBeInTheDocument()
    a.unmount()
    setup({ live: false })
    expect(screen.queryByRole('button', { name: 'Mais ações' })).toBeNull()
  })

  it('Interagir chama setReadonly(false) e o rótulo troca com onMode', () => {
    setup()
    fireEvent.click(screen.getByRole('button', { name: 'Mais ações' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Interagir' }))
    expect(handle.setReadonly).toHaveBeenCalledWith(false)
    act(() => last!.onMode!(false))
    fireEvent.click(screen.getByRole('button', { name: 'Mais ações' }))
    expect(screen.getByRole('menuitem', { name: 'Somente assistir' })).toBeInTheDocument()
  })

  it('Encerrar agente pede confirmação no tile e só então chama kill', () => {
    const confirmSpy = vi.spyOn(window, 'confirm')
    setup()
    fireEvent.click(screen.getByRole('button', { name: 'Mais ações' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Encerrar agente' }))
    expect(handle.kill).not.toHaveBeenCalled()
    expect(screen.getByText('Encerrar o agente e o terminal?')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(screen.queryByText('Encerrar o agente e o terminal?')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Mais ações' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Encerrar agente' }))
    fireEvent.click(screen.getByRole('button', { name: 'Sim, encerrar' }))
    expect(handle.kill).toHaveBeenCalledOnce()
    expect(confirmSpy).not.toHaveBeenCalled()
  })

  it('conexão perdida mostra Desconectado e Tentar de novo; terminal encerrado nunca', () => {
    setup()
    expect(screen.queryByRole('button', { name: 'Tentar de novo' })).toBeNull()
    act(() => last!.onStatus!('desconectado'))
    expect(screen.getByText('Desconectado')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(handle.reconnect).toHaveBeenCalledOnce()
    // O processo terminou: onStatus('desconectado') e onEnded chegam juntos.
    act(() => last!.onEnded!())
    expect(screen.queryByRole('button', { name: 'Tentar de novo' })).toBeNull()
    expect(screen.queryByText('Desconectado')).toBeNull()
    expect(screen.getByText(/encerrado/)).toBeInTheDocument()
  })

  it('dragstart no cabeçalho e dragover na metade direita', () => {
    const { props, container } = setup()
    const section = container.querySelector('section') as HTMLElement
    vi.spyOn(section, 'getBoundingClientRect').mockReturnValue({ left: 0, width: 400, top: 0, height: 360, right: 400, bottom: 360, x: 0, y: 0, toJSON() {} })
    fireEvent.dragStart(screen.getByTestId('tile-header'))
    expect(props.onDragStart).toHaveBeenCalledOnce()
    const over = (x: number) => {
      const e = new Event('dragover', { bubbles: true, cancelable: true })
      Object.defineProperty(e, 'clientX', { value: x })
      fireEvent(section, e)
    }
    over(300)
    expect(props.onDragOverTile).toHaveBeenLastCalledWith(true)
    over(100)
    expect(props.onDragOverTile).toHaveBeenLastCalledWith(false)
    fireEvent.dragEnd(screen.getByTestId('tile-header'))
    expect(props.onDragEnd).toHaveBeenCalledOnce()
  })

  it('gesto no canto muda só o estilo do tile e chama onResize uma vez ao soltar', () => {
    const { props, container } = setup()
    const section = container.querySelector('section') as HTMLElement
    const parent = section.parentElement as HTMLElement
    Object.defineProperty(parent, 'clientWidth', { value: 1200 }) // 3 colunas de 400, sem vão
    vi.spyOn(section, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 400, height: 360, right: 400, bottom: 360, x: 0, y: 0, toJSON() {} })
    const corner = screen.getByRole('button', { name: 'Redimensionar terminal' })
    const ev = (type: string, x: number, y: number) => {
      const e = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperties(e, { clientX: { value: x }, clientY: { value: y }, pointerId: { value: 1 } })
      return e
    }
    fireEvent(corner, ev('pointerdown', 400, 360))
    expect(handle.holdFit).toHaveBeenCalledOnce()
    fireEvent(corner, ev('pointermove', 800, 2000))
    expect(section.style.height).toBe('900px')
    expect(section.style.gridColumn).toBe('span 2')
    expect(screen.getByText('2 de 3 col · 900 px')).toBeInTheDocument()
    expect(props.onResize).not.toHaveBeenCalled()
    expect(handle.releaseFit).not.toHaveBeenCalled()
    fireEvent(corner, ev('pointerup', 800, 2000))
    expect(props.onResize).toHaveBeenCalledOnce()
    expect(props.onResize).toHaveBeenCalledWith({ span: 2, h: 900 })
    // Um ajuste só, ao soltar: releaseFit já ajusta, sem fit() a mais.
    expect(handle.releaseFit).toHaveBeenCalledOnce()
    expect(handle.fit).not.toHaveBeenCalled()
    expect(screen.queryByText(/de 3 col/)).toBeNull()
  })

  it('pointercancel no canto devolve o tamanho e solta o ajuste segurado', () => {
    const { props, container } = setup()
    const section = container.querySelector('section') as HTMLElement
    Object.defineProperty(section.parentElement as HTMLElement, 'clientWidth', { value: 1200 })
    vi.spyOn(section, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 400, height: 360, right: 400, bottom: 360, x: 0, y: 0, toJSON() {} })
    const corner = screen.getByRole('button', { name: 'Redimensionar terminal' })
    const ev = (type: string, x: number, y: number) => {
      const e = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperties(e, { clientX: { value: x }, clientY: { value: y }, pointerId: { value: 1 } })
      return e
    }
    fireEvent(corner, ev('pointerdown', 400, 360))
    expect(handle.holdFit).toHaveBeenCalledOnce()
    fireEvent(corner, ev('pointermove', 800, 2000))
    fireEvent(corner, ev('pointercancel', 800, 2000))
    expect(handle.releaseFit).toHaveBeenCalledOnce()
    expect(props.onResize).not.toHaveBeenCalled()
    expect(section.style.height).toBe('360px')
    expect(section.style.gridColumn).toBe('span 1')
  })
})

describe('contexto no tile', () => {
  const ctx = { sessionId: 's1', ctxPct: 62, ctxTokens: 124000, ctxWindow: 200000 }
  const none = () => {
    expect(screen.queryByTestId('ctx-chip')).toBeNull()
    expect(screen.queryByTestId('ctx-strip')).toBeNull()
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
  }

  it('sem as props novas não mostra nada do medidor', () => {
    setup()
    none()
  })

  it('detalhado mostra a faixa com Limpar, sem chip nem aviso', () => {
    setup(ctx)
    expect(screen.getByTestId('ctx-strip')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Limpar' })).toBeInTheDocument()
    expect(screen.queryByTestId('ctx-chip')).toBeNull()
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
  })

  it('simples mostra o chip antes do estado, sem faixa', () => {
    setup({ ...ctx, simple: true })
    const chip = screen.getByTestId('ctx-chip')
    const state = screen.getByText('conectando')
    expect(chip.compareDocumentPosition(state) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByTestId('ctx-strip')).toBeNull()
  })

  it.each([false, true])('aviso nos dois modos (simples=%s)', (simple) => {
    const { unmount } = setup({ ...ctx, simple, ctxPct: 84 })
    expect(screen.getByTestId('ctx-warning')).toHaveAttribute('data-level', 'warn')
    unmount()
    setup({ ...ctx, simple, ctxPct: 92 })
    expect(screen.getByTestId('ctx-warning')).toHaveAttribute('data-level', 'crit')
  })

  it('limite 95 com 92% não avisa', () => {
    setup({ ...ctx, ctxPct: 92, ctxLimit: 95 })
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
  })

  it('sessão sem card: faixa sem Limpar, sem aviso, e o cabeçalho diz sem card', () => {
    setup({ ...ctx, cardKey: '', ctxPct: 84, t: term({ card_key: 'NAC-7' }) })
    expect(screen.getByTestId('ctx-strip')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Limpar' })).toBeNull()
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
    expect(screen.getByText('sem card')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'NAC-7' })).toBeNull()
  })

  it('sem sessão: sem Limpar e sem aviso', () => {
    setup({ ...ctx, sessionId: '', ctxPct: 84 })
    expect(screen.queryByRole('button', { name: 'Limpar' })).toBeNull()
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
  })

  it('cardKey vale no lugar do card do terminal', () => {
    setup({ ...ctx, cardKey: 'NAC-9', t: term({ card_key: '' }) })
    expect(screen.getByRole('link', { name: 'NAC-9' })).toHaveAttribute('href', '/cards/NAC-9')
    expect(screen.getByLabelText('Terminal Claude Code do card NAC-9')).toBeInTheDocument()
  })

  it('terminal encerrado não mostra o medidor', () => {
    setup({ ...ctx, ctxPct: 92, t: term({ ended_at: new Date().toISOString(), exit_code: 0 }) })
    none()
  })

  it('sem dado (ctxPct nulo) não mostra o medidor', () => {
    setup({ ...ctx, ctxPct: null })
    none()
  })

  it('tile em pausa também mostra a faixa', () => {
    setup({ ...ctx, live: false })
    expect(screen.getByTestId('ctx-strip')).toBeInTheDocument()
  })
})

describe('Limpar e continuar', () => {
  const URL = '/api/sessions/s1/restart-clean'
  const MSG = 'limite de terminais atingido — o card ficou pausado; use Continuar no card para abrir de novo.'
  function mock(routes: Record<string, unknown>) {
    const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
      const r = routes[`${init?.method ?? 'GET'} ${url}`]
      if (r === undefined) return new Response('{}', { status: 404 })
      if (r && typeof r === 'object' && 'status' in r && 'body' in r) {
        const o = r as { status: number; body: unknown }
        return new Response(JSON.stringify(o.body), { status: o.status })
      }
      return new Response(JSON.stringify(r), { status: 200 })
    })
    vi.stubGlobal('fetch', fn)
    return fn
  }
  const bodies = (fn: ReturnType<typeof mock>, url = URL) =>
    fn.mock.calls.filter(([u, i]) => u === url && i?.method === 'POST').map(([, i]) => JSON.parse(i?.body ?? '{}'))
  const base = (): Partial<GridTileProps> => ({
    sessionId: 's1', cardKey: 'NAC-7', ctxPct: 84, ctxTokens: 168000, ctxWindow: 200000,
    whereStopped: 'Parei no passo 3', whereStoppedAt: new Date().toISOString(), onRestarted: vi.fn(),
  })
  const panel = () => screen.queryByRole('region', { name: 'Recomeçar limpo' })
  const confirm = () => within(panel()!).getByRole('button', { name: 'Limpar e continuar' })
  const openMenu = () => fireEvent.click(screen.getByRole('button', { name: 'Mais ações' }))
  afterEach(() => vi.unstubAllGlobals())

  it('menu ⋯: habilitado com card, desabilitado sem card, ausente sem sessão', () => {
    const a = setup(base())
    openMenu()
    expect(screen.getByRole('menuitem', { name: 'Limpar e continuar' })).toBeEnabled()
    a.unmount()
    const b = setup({ ...base(), cardKey: '' })
    openMenu()
    expect(screen.getByRole('menuitem', { name: 'Limpar e continuar (precisa de um card)' })).toBeDisabled()
    b.unmount()
    setup({ ...base(), sessionId: '' })
    openMenu()
    expect(screen.queryByRole('menuitem', { name: /^Limpar e continuar/ })).toBeNull()
  })

  it('o Limpar da faixa abre o painel sem enviar nada', () => {
    const fn = mock({})
    setup(base())
    expect(panel()).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    expect(panel()).toBeInTheDocument()
    expect(screen.getByText('A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo, com o contexto zerado.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Onde parei' })).toHaveValue('Parei no passo 3')
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Limpar' })).toBeNull()
    // O terminal segue montado por baixo do painel.
    expect(screen.getByTestId('fake-term')).toBeInTheDocument()
    expect(bodies(fn)).toEqual([])
  })

  it('a linha de aviso e o menu abrem o mesmo painel; Cancelar fecha', () => {
    setup(base())
    fireEvent.click(within(screen.getByTestId('ctx-warning')).getByRole('button', { name: 'Limpar e continuar' }))
    expect(panel()).toBeInTheDocument()
    fireEvent.click(within(panel()!).getByRole('button', { name: 'Cancelar' }))
    expect(panel()).toBeNull()
    expect(screen.getByTestId('ctx-warning')).toBeInTheDocument()
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Limpar e continuar' }))
    expect(screen.queryByRole('menu')).toBeNull()
    expect(panel()).toBeInTheDocument()
  })

  it('confirmar envia mode background; sucesso avisa e oferece encerrar com confirmação', async () => {
    const fn = mock({ [`POST ${URL}`]: { ok: true, terminal_id: 't9' } })
    const { props } = setup(base())
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    fireEvent.click(confirm())
    await screen.findByText('Contexto limpo. O agente novo abriu em outro terminal.')
    expect(bodies(fn)).toEqual([{ mode: 'background' }])
    expect(props.onRestarted).toHaveBeenCalledOnce()
    expect(props.onRestarted).toHaveBeenCalledWith('t9')
    expect(panel()).toBeNull()
    expect(screen.queryByTestId('ctx-warning')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Limpar' })).toBeNull()
    expect(handle.kill).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Encerrar este terminal' }))
    expect(screen.getByText('Encerrar o agente e o terminal?')).toBeInTheDocument()
    expect(handle.kill).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Sim, encerrar' }))
    expect(handle.kill).toHaveBeenCalledOnce()
  })

  it('Fechar tira o aviso de contexto limpo', async () => {
    mock({ [`POST ${URL}`]: { ok: true, terminal_id: 't9' } })
    setup(base())
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    fireEvent.click(confirm())
    await screen.findByText('Contexto limpo. O agente novo abriu em outro terminal.')
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    expect(screen.queryByText('Contexto limpo. O agente novo abriu em outro terminal.')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Encerrar este terminal' })).toBeNull()
  })

  it('registro velho: aviso e confirmar desabilitado com o texto vazio', () => {
    setup({ ...base(), whereStoppedAt: null })
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    expect(screen.getByText('O registro está velho ou vazio: confirme ou atualize o texto antes de recomeçar.')).toBeInTheDocument()
    expect(confirm()).toBeEnabled()
    fireEvent.change(screen.getByRole('textbox', { name: 'Onde parei' }), { target: { value: '  ' } })
    expect(confirm()).toBeDisabled()
  })

  it('erro depois de desligar o card: o painel segue aberto com o erro do servidor', async () => {
    mock({ [`POST ${URL}`]: { status: 409, body: { error: MSG } } })
    const { props, rerenderWith } = setup(base())
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    fireEvent.click(confirm())
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(MSG))
    rerenderWith({ cardKey: '' }) // a sessão perdeu o card
    expect(panel()).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent(MSG)
    expect(screen.getByRole('alert').textContent).toBe(MSG)
    expect(props.onRestarted).not.toHaveBeenCalled()
    expect(screen.queryByText('Contexto limpo. O agente novo abriu em outro terminal.')).toBeNull()
  })

  it('Cancelar fica desabilitado enquanto o pedido está pendente', async () => {
    let reply: (r: Response) => void = () => {}
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((res) => { reply = res })))
    setup(base())
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    const cancel = within(panel()!).getByRole('button', { name: 'Cancelar' })
    expect(cancel).toBeEnabled()
    fireEvent.click(confirm())
    await waitFor(() => expect(within(panel()!).getByRole('button', { name: 'Abrindo…' })).toBeInTheDocument())
    expect(cancel).toBeDisabled()
    fireEvent.click(cancel)
    expect(panel()).toBeInTheDocument()
    await act(async () => { reply(new Response(JSON.stringify({ error: MSG }), { status: 409 })) })
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(MSG))
    expect(within(panel()!).getByRole('button', { name: 'Cancelar' })).toBeEnabled()
  })

  it('sessão trocada com o painel aberto: o POST vai para a sessão em que abriu', async () => {
    const fn = mock({ [`POST ${URL}`]: { status: 409, body: { error: 'a sessão não tem card' } } })
    const { rerenderWith } = setup(base())
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    rerenderWith({ sessionId: 's2', cardKey: '' })
    expect(panel()).toBeInTheDocument()
    fireEvent.click(confirm())
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('a sessão não tem card'))
    expect(bodies(fn)).toEqual([{ mode: 'background' }])
    expect(bodies(fn, '/api/sessions/s2/restart-clean')).toEqual([])
    expect(panel()).toBeInTheDocument()
  })

  it('sucesso com o tile em pausa: avisa, sem oferecer encerrar', async () => {
    mock({ [`POST ${URL}`]: { ok: true, terminal_id: 't9' } })
    setup({ ...base(), live: false })
    fireEvent.click(screen.getByRole('button', { name: 'Limpar' }))
    fireEvent.click(confirm())
    await screen.findByText('Contexto limpo. O agente novo abriu em outro terminal.')
    expect(screen.queryByRole('button', { name: 'Encerrar este terminal' })).toBeNull()
  })
})

describe('barra de entrada no tile', () => {
  const bar = () => screen.queryByRole('textbox', { name: 'Enviar ao agente Claude Code' })

  it('tile ligado mostra a barra; em pausa e encerrado não', () => {
    const a = setup()
    expect(bar()).toBeInTheDocument()
    a.unmount()
    const b = setup({ live: false })
    expect(bar()).toBeNull()
    b.unmount()
    setup({ t: term({ ended_at: new Date().toISOString(), exit_code: 0 }) })
    expect(bar()).toBeNull()
  })

  it('terminal de nobre run mostra Liberar entrada em vez da barra', () => {
    setup({ t: term({ origin: 'nobre run' }) })
    expect(bar()).toBeNull()
    expect(screen.getByRole('button', { name: 'Liberar entrada' })).toBeInTheDocument()
  })

  it('digitar na barra não chama callback do tile além de onFocus', () => {
    const { props } = setup()
    ;(props.onFocus as ReturnType<typeof vi.fn>).mockClear()
    fireEvent.change(bar()!, { target: { value: 'oi' } })
    const calls = Object.entries(props).filter(([, v]) => typeof v === 'function' && (v as ReturnType<typeof vi.fn>).mock?.calls.length)
    expect(calls.map(([k]) => k).filter((k) => k !== 'onFocus')).toEqual([])
  })

  it('pausa e volta: o texto digitado volta na barra; em pausa não há registro', () => {
    const { rerenderWith } = setup()
    fireEvent.change(bar()!, { target: { value: 'meio escrito' } })
    rerenderWith({ live: false })
    expect(bar()).toBeNull()
    expect(inputFor('t1')).toBeNull()
    rerenderWith({ live: true })
    expect(bar()).toHaveValue('meio escrito')
    expect(inputFor('t1')).not.toBeNull()
  })

  it('Liberar entrada de um nobre run continua valendo depois da pausa', () => {
    const run = term({ origin: 'nobre run' })
    const { rerenderWith } = setup({ t: run })
    fireEvent.click(screen.getByRole('button', { name: 'Liberar entrada' }))
    fireEvent.change(bar()!, { target: { value: 'oi' } })
    rerenderWith({ t: run, live: false })
    expect(screen.queryByRole('button', { name: 'Liberar entrada' })).toBeNull()
    rerenderWith({ t: run, live: true })
    expect(screen.queryByRole('button', { name: 'Liberar entrada' })).toBeNull()
    expect(bar()).toHaveValue('oi')
  })

  it('terminal encerrado esquece o rascunho', () => {
    const { rerenderWith } = setup()
    fireEvent.change(bar()!, { target: { value: 'oi' } })
    rerenderWith({ t: term({ ended_at: new Date().toISOString(), exit_code: 0 }) })
    expect(bar()).toBeNull()
    expect(draftFor('t1')).toEqual({ text: '', freed: false })
  })

  it('o corpo marca data-interactive só no modo Interagir', () => {
    setup()
    const body = () => screen.getByTestId('fake-term').parentElement as HTMLElement
    expect(body()).not.toHaveAttribute('data-interactive')
    act(() => last!.onMode!(false))
    expect(body()).toHaveAttribute('data-interactive', '1')
  })
})
