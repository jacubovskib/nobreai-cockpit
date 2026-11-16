import '@testing-library/jest-dom/vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { BASE_FONT, MIN_FONT, TerminalView, fontToFit, type TermLike, type WsLike } from './TerminalView'

class FakeTerm implements TermLike {
  cols = 100
  rows = 30
  written: Uint8Array[] = []
  writeCbs: (() => void)[] = []
  resized: [number, number][] = []
  fits = 0
  fontSize = 13
  fonts: number[] = []
  resets = 0
  disposed = false
  dataCb: (d: string) => void = () => {}
  open() {}
  write(d: Uint8Array | string, cb?: () => void) {
    this.written.push(typeof d === 'string' ? new TextEncoder().encode(d) : d)
    if (cb) this.writeCbs.push(cb)
  }
  // flush simula o xterm terminando de processar o que foi escrito.
  flush() { const cbs = this.writeCbs; this.writeCbs = []; cbs.forEach((cb) => cb()) }
  resize(c: number, r: number) { this.cols = c; this.rows = r; this.resized.push([c, r]) }
  reset() { this.resets++ }
  fit() { this.fits++ }
  focus() {}
  onData(cb: (d: string) => void) { this.dataCb = cb }
  resizeCb: () => void = () => {}
  onResize(cb: () => void) { this.resizeCb = cb }
  dispose() { this.disposed = true }
}
class FakeWs implements WsLike {
  binaryType = ''
  readyState = 0
  sent: unknown[] = []
  closed = false
  onopen: ((e: unknown) => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: ((e: unknown) => void) | null = null
  onerror: ((e: unknown) => void) | null = null
  constructor(public url: string) {}
  send(d: unknown) { this.sent.push(d) }
  close() { this.closed = true }
  open() { this.readyState = 1; this.onopen?.({}) }
  msg(data: unknown) { this.onmessage?.({ data }) }
  drop() { this.readyState = 3; this.onclose?.({}) }
}

function setup(props: { readonly?: boolean } = {}) {
  const term = new FakeTerm()
  const socks: FakeWs[] = []
  const wsFactory = (url: string) => { const w = new FakeWs(url); socks.push(w); return w }
  const r = render(<TerminalView id="t1" termFactory={() => term} wsFactory={wsFactory} {...props} />)
  return { term, socks, ...r }
}
const text = (b: Uint8Array) => new TextDecoder().decode(b)

afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

const snapshot = (w: FakeWs, t: FakeTerm, data = 'hist') => {
  act(() => w.msg(new TextEncoder().encode(data).buffer))
  act(() => t.flush())
}
const resizes = (w: FakeWs) => w.sent.filter((d) => typeof d === 'string').map((d) => JSON.parse(d as string))
const host = () => screen.getByRole('group', { name: 'Terminal do agente' })

describe('TerminalView', () => {
  it('conecta no ws do terminal, escreve bytes binários e não envia resize ao abrir', () => {
    const { term, socks } = setup()
    expect(socks[0].url).toBe(`ws://${window.location.host}/api/terminals/t1/ws`)
    act(() => socks[0].open())
    act(() => socks[0].msg(new TextEncoder().encode('olá').buffer))
    expect(text(term.written[0])).toBe('olá')
    expect(socks[0].sent).toHaveLength(0)
  })

  it('fontToFit diminui a fonte até o PTY caber, entre o mínimo e a base', () => {
    expect(fontToFit(13, 1000, 539, 920, 680)).toBe(10) // 13 × 539/680 = 10,3
    expect(fontToFit(13, 2000, 2000, 920, 680)).toBe(BASE_FONT) // nunca passa da base
    expect(fontToFit(10, 1200, 900, 708, 523)).toBe(13) // contêiner cresceu: volta a crescer até a base
    expect(fontToFit(13, 100, 100, 920, 680)).toBe(MIN_FONT) // PTY enorme: para no mínimo
    expect(fontToFit(13, 0, 0, 920, 680)).toBe(13) // contêiner ainda sem tamanho
    expect(fontToFit(13, 800, 600, 0, 0)).toBe(13) // terminal ainda sem tamanho
  })

  // A tela do xterm escala com a fonte (natural = medida na fonte base), como no xterm real.
  const sizes = (h: HTMLElement, screenEl: HTMLElement, natural: { w: number; h: number }, term: FakeTerm) => {
    Object.defineProperty(h, 'clientWidth', { configurable: true, value: 1016 })
    Object.defineProperty(h, 'clientHeight', { configurable: true, value: 555 }) // 539 úteis (padding 8 + 8)
    const xs = document.createElement('div')
    xs.className = 'xterm-screen'
    screenEl.appendChild(xs)
    Object.defineProperty(xs, 'offsetWidth', { configurable: true, get: () => natural.w * term.fontSize / BASE_FONT })
    Object.defineProperty(xs, 'offsetHeight', { configurable: true, get: () => natural.h * term.fontSize / BASE_FONT })
  }

  it('seguindo o PTY, um terminal maior que o contêiner ganha fonte menor para caber inteiro', () => {
    const { term, socks } = setup()
    const h = host()
    const screenEl = h.firstElementChild as HTMLElement
    expect(screenEl.style.width).toBe('100%') // o pai do xterm ocupa o contêiner (FitAddon mede o pai)
    expect(screenEl.style.height).toBe('100%')
    sizes(h, screenEl, { w: 920, h: 680 }, term)
    act(() => socks[0].open())
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 120, rows: 40 })))
    expect(term.fontSize).toBe(10)
    expect(term.resized.at(-1)).toEqual([120, 40]) // linhas e colunas continuam as do PTY
    expect(screenEl.style.transform).toBe('') // sem transform: mouse e seleção alinhados
  })

  it('ao interagir a fonte volta à base antes do ajuste ao contêiner', () => {
    const { term, socks } = setup()
    const h = host()
    sizes(h, h.firstElementChild as HTMLElement, { w: 920, h: 680 }, term)
    act(() => socks[0].open())
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 120, rows: 40 })))
    expect(term.fontSize).toBe(10)
    fireEvent.keyDown(h, { key: 'a' })
    expect(term.fontSize).toBe(BASE_FONT)
    expect(term.fits).toBeGreaterThan(0)
  })

  it('o botão Interagir (reconexão sem seguir o PTY) também volta a fonte à base', () => {
    const { term, socks } = setup({ readonly: true })
    const h = host()
    sizes(h, h.firstElementChild as HTMLElement, { w: 920, h: 680 }, term)
    act(() => socks[0].open())
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 120, rows: 40 })))
    expect(term.fontSize).toBe(10)
    fireEvent.click(screen.getByRole('button', { name: 'Interagir' }))
    act(() => socks[1].open())
    expect(term.fontSize).toBe(BASE_FONT)
  })

  it('usa o tamanho do PTY (mensagem info) até o usuário interagir, sem fit', () => {
    const { term, socks } = setup()
    act(() => socks[0].open())
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 132, rows: 43 })))
    expect(term.resized.at(-1)).toEqual([132, 43])
    expect(term.fits).toBe(0)
    expect(socks[0].sent).toHaveLength(0)
  })

  it('ignora onData até o snapshot ser escrito (respostas do histórico não viram tecla)', () => {
    const { term, socks } = setup()
    act(() => socks[0].open())
    act(() => socks[0].msg(new TextEncoder().encode('\x1b[c').buffer))
    term.dataCb('\x1b[?1;2c') // resposta do xterm à consulta DA do histórico
    expect(socks[0].sent).toHaveLength(0)
    act(() => term.flush())
    term.dataCb('ls')
    expect(text(socks[0].sent.at(-1) as Uint8Array)).toBe('ls')
  })

  it('snapshot vazio libera a entrada na hora', () => {
    const { term, socks } = setup()
    act(() => socks[0].open())
    act(() => socks[0].msg(new ArrayBuffer(0)))
    term.dataCb('x')
    expect(text(socks[0].sent.at(-1) as Uint8Array)).toBe('x')
  })

  it('digitar em modo interativo ajusta ao contêiner e envia o resize antes das teclas', () => {
    const { term, socks } = setup()
    act(() => socks[0].open())
    snapshot(socks[0], term)
    expect(resizes(socks[0])).toHaveLength(0)
    fireEvent.keyDown(host(), { key: 'l' })
    term.dataCb('l')
    expect(term.fits).toBeGreaterThan(0)
    expect(resizes(socks[0])).toEqual([{ type: 'resize', cols: 100, rows: 30 }])
    expect(text(socks[0].sent.at(-1) as Uint8Array)).toBe('l')
  })

  it('em somente assistir não envia teclas nem resize, usa ?readonly=1 e não faz fit', () => {
    vi.useFakeTimers()
    const { term, socks } = setup({ readonly: true })
    expect(socks[0].url).toMatch(/\?readonly=1$/)
    act(() => socks[0].open())
    snapshot(socks[0], term)
    fireEvent.keyDown(host(), { key: 'x' })
    term.dataCb('x')
    act(() => { term.resizeCb() })
    act(() => { window.dispatchEvent(new Event('resize')) })
    act(() => { vi.advanceTimersByTime(500) })
    expect(socks[0].sent).toHaveLength(0)
    expect(term.fits).toBe(0)
  })

  it('Interagir reconecta sem readonly e só então envia o resize', () => {
    const { term, socks } = setup({ readonly: true })
    act(() => socks[0].open())
    fireEvent.click(screen.getByRole('button', { name: 'Interagir' }))
    expect(socks).toHaveLength(2)
    expect(socks[1].url).not.toMatch(/readonly/)
    act(() => socks[1].open())
    expect(term.fits).toBeGreaterThan(0)
    expect(resizes(socks[1])).toEqual([{ type: 'resize', cols: 100, rows: 30 }])
    expect(screen.getByRole('button', { name: 'Somente assistir' })).toBeInTheDocument()
  })

  it('o botão alterna para somente assistir reconectando', () => {
    const { socks } = setup()
    act(() => socks[0].open())
    fireEvent.click(screen.getByRole('button', { name: 'Somente assistir' }))
    expect(socks).toHaveLength(2)
    expect(socks[1].url).toMatch(/\?readonly=1$/)
    expect(screen.getByRole('button', { name: 'Interagir' })).toBeInTheDocument()
  })

  it('mostra encerrado (código N) ao receber exit e não reconecta', () => {
    vi.useFakeTimers()
    const { socks } = setup()
    act(() => socks[0].open())
    act(() => socks[0].msg(JSON.stringify({ type: 'exit', code: 0, reason: 'agent' })))
    expect(screen.getByText('encerrado (código 0)')).toBeInTheDocument()
    act(() => socks[0].drop())
    act(() => { vi.advanceTimersByTime(10000) })
    expect(socks).toHaveLength(1)
  })

  it('reconecta até 3 vezes com backoff em queda anormal', () => {
    vi.useFakeTimers()
    const { socks, term } = setup()
    act(() => socks[0].open())
    act(() => socks[0].drop())
    expect(screen.getByRole('status')).toHaveTextContent('reconectando')
    act(() => { vi.advanceTimersByTime(500) })
    expect(socks).toHaveLength(2)
    act(() => socks[1].drop())
    act(() => { vi.advanceTimersByTime(1000) })
    expect(socks).toHaveLength(3)
    act(() => socks[2].drop())
    act(() => { vi.advanceTimersByTime(2000) })
    expect(socks).toHaveLength(4)
    act(() => socks[3].drop())
    act(() => { vi.advanceTimersByTime(60000) })
    expect(socks).toHaveLength(4)
    expect(screen.getByRole('status')).toHaveTextContent('desconectado')
    act(() => socks[3].open())
    expect(term.resets).toBeGreaterThan(0)
  })

  it('a cada reconexão a entrada espera o novo snapshot', () => {
    vi.useFakeTimers()
    const { socks, term } = setup()
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => socks[0].drop())
    act(() => { vi.advanceTimersByTime(500) })
    act(() => socks[1].open())
    term.dataCb('cedo')
    expect(socks[1].sent).toHaveLength(0)
  })

  it('Encerrar agente pede confirmação inline e chama DELETE', async () => {
    const fetchMock = vi.fn(async () => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    setup()
    fireEvent.click(screen.getByRole('button', { name: 'Encerrar agente' }))
    expect(fetchMock).not.toHaveBeenCalled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Sim, encerrar' })) })
    expect(fetchMock).toHaveBeenCalledWith('/api/terminals/t1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('depois de interagir, o resize é debounced em 100 ms', () => {
    vi.useFakeTimers()
    const a = setup()
    act(() => a.socks[0].open())
    snapshot(a.socks[0], a.term)
    a.term.resizeCb()
    act(() => { vi.advanceTimersByTime(500) })
    expect(resizes(a.socks[0])).toHaveLength(0) // ainda não interagiu
    fireEvent.keyDown(host(), { key: 'a' })
    const base = a.socks[0].sent.length
    a.term.cols = 120
    act(() => { a.term.resizeCb(); a.term.resizeCb() })
    act(() => { vi.advanceTimersByTime(99) })
    expect(a.socks[0].sent).toHaveLength(base)
    act(() => { vi.advanceTimersByTime(1) })
    expect(a.socks[0].sent).toHaveLength(base + 1)
    expect(JSON.parse(a.socks[0].sent.at(-1) as string)).toMatchObject({ cols: 120 })
  })

  it('fecha o socket antigo ao alternar somente assistir e ao trocar de id', () => {
    const term = new FakeTerm()
    const socks: FakeWs[] = []
    const wsFactory = (url: string) => { const w = new FakeWs(url); socks.push(w); return w }
    const ui = (id: string) => <TerminalView id={id} termFactory={() => term} wsFactory={wsFactory} />
    const { rerender } = render(ui('t1'))
    fireEvent.click(screen.getByRole('button', { name: 'Somente assistir' }))
    expect(socks[0].closed).toBe(true)
    rerender(ui('t2'))
    expect(socks[1].closed).toBe(true)
    expect(socks[2].url).toContain('/t2/ws')
  })

  it('não foca o xterm sem autoFocus', () => {
    const term = new FakeTerm()
    const focus = vi.spyOn(term, 'focus')
    const ws = new FakeWs('x')
    render(<TerminalView id="t1" termFactory={() => term} wsFactory={() => ws} />)
    act(() => ws.open())
    expect(focus).not.toHaveBeenCalled()
  })

  it('fecha o socket e descarta o terminal ao desmontar', () => {
    const { socks, term, unmount } = setup()
    unmount()
    expect(socks[0].closed).toBe(true)
    expect(term.disposed).toBe(true)
  })
})
