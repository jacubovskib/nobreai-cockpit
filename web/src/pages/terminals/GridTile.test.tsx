import { forwardRef, useImperativeHandle } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { TerminalInfo } from '../../api/terminals'
import type { TerminalHandle, TerminalViewProps } from '../../ui/TerminalView'
import { GridTile, type GridTileProps } from './GridTile'

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

beforeEach(() => { Object.values(handle).forEach((f) => f.mockClear()); last = null })

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
