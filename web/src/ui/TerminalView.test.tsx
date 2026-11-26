import '@testing-library/jest-dom/vitest'
import { createRef } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { wsUrl } from '../api/terminals'
import { LazyTerminalView } from './LazyTerminalView'
import { BASE_FONT, MIN_FONT, TerminalView, fontToFit, xtermFactory, type TerminalHandle, type TermLike, type WsLike } from './TerminalView'

// WebglAddon simulado: o jsdom não tem WebGL. `mode` escolhe a falha.
const webgl = vi.hoisted(() => ({
  mode: 'ok' as 'ok' | 'ctor' | 'activate',
  loseContext: null as null | (() => void),
  disposed: 0,
}))
vi.mock('@xterm/addon-webgl', () => ({
  WebglAddon: class {
    constructor() { if (webgl.mode === 'ctor') throw new Error('sem WebGL2') }
    onContextLoss(cb: () => void) { webgl.loseContext = cb; return { dispose() {} } }
    activate() { if (webgl.mode === 'activate') throw new Error('WebGL2 not supported') }
    dispose() { webgl.disposed++ }
  },
}))

class FakeTerm implements TermLike {
  cols = 100
  rows = 30
  written: Uint8Array[] = []
  writeCbs: (() => void)[] = []
  resized: [number, number][] = []
  fits = 0
  fontSize = 13
  cursorBlink = true
  renderer: 'dom' | 'webgl' = 'dom'
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

describe('wsUrl', () => {
  const loc = { protocol: 'https:', host: 'h:1' }
  it('acrescenta batch=1 à query só quando pedido', () => {
    expect(wsUrl('t1', false, loc)).toBe('wss://h:1/api/terminals/t1/ws')
    expect(wsUrl('t1', true, loc)).toBe('wss://h:1/api/terminals/t1/ws?readonly=1')
    expect(wsUrl('t1', true, loc, true)).toBe('wss://h:1/api/terminals/t1/ws?readonly=1&batch=1')
    expect(wsUrl('t1', false, loc, true)).toBe('wss://h:1/api/terminals/t1/ws?batch=1')
  })
})

describe('TerminalView na grade', () => {
  function gridSetup(props: { readonly?: boolean; active?: boolean; onMode?: (ro: boolean) => void; onStatus?: (s: string) => void } = {}) {
    const term = new FakeTerm()
    const socks: FakeWs[] = []
    const factory = vi.fn((_o?: unknown) => term)
    const wsFactory = (url: string) => { const w = new FakeWs(url); socks.push(w); return w }
    const ref = createRef<TerminalHandle>()
    const ui = (p: typeof props) => <TerminalView ref={ref} id="t1" grid termFactory={factory} wsFactory={wsFactory} {...p} />
    const r = render(ui(props))
    return { term, socks, factory, ref, rerender: (p: typeof props) => r.rerender(ui(p)) }
  }

  it('sem grid: sem batch=1, fábrica com o padrão de hoje e a barra de botões aparece', () => {
    const term = new FakeTerm()
    const factory = vi.fn((_o?: unknown) => term)
    const socks: FakeWs[] = []
    render(<TerminalView id="t1" readonly termFactory={factory} wsFactory={(u) => { const w = new FakeWs(u); socks.push(w); return w }} />)
    expect(socks[0].url).not.toMatch(/batch/)
    expect(factory).toHaveBeenCalledWith()
    expect(term.cursorBlink).toBe(true)
    expect(screen.getByRole('button', { name: 'Interagir' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Encerrar agente' })).toBeInTheDocument()
  })

  it('com grid a fábrica recebe scrollback 1000 e WebGL', () => {
    const { factory } = gridSetup({ readonly: true })
    expect(factory).toHaveBeenCalledWith(expect.objectContaining({ scrollback: 1000, webgl: true }))
  })

  it('com grid não renderiza a barra de botões própria', () => {
    gridSetup({ readonly: true })
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.queryByRole('status')).toBeNull()
    expect(host()).toBeInTheDocument()
  })

  it('assistindo pede readonly=1 e batch=1; setReadonly(false) reconecta sem nenhum dos dois', () => {
    const onMode = vi.fn()
    const { socks, ref, term } = gridSetup({ readonly: true, onMode })
    expect(socks[0].url).toMatch(/\?readonly=1&batch=1$/)
    act(() => ref.current!.setReadonly(false))
    expect(socks[0].closed).toBe(true)
    expect(socks).toHaveLength(2)
    expect(socks[1].url).not.toMatch(/readonly|batch/)
    expect(onMode).toHaveBeenLastCalledWith(false)
    // conta como "Interagir": ao abrir, ajusta ao contêiner e manda o resize
    act(() => socks[1].open())
    expect(term.fits).toBeGreaterThan(0)
    expect(resizes(socks[1])).toEqual([{ type: 'resize', cols: 100, rows: 30 }])
  })

  it('onMode avisa só a troca, não o modo inicial', () => {
    const onMode = vi.fn()
    const { ref } = gridSetup({ readonly: true, onMode })
    expect(onMode).not.toHaveBeenCalled()
    act(() => ref.current!.setReadonly(true))
    expect(onMode).not.toHaveBeenCalled()
    act(() => ref.current!.setReadonly(false))
    act(() => ref.current!.setReadonly(true))
    expect(onMode.mock.calls).toEqual([[false], [true]])
  })

  it('três mensagens antes do próximo quadro viram um único write; o snapshot não espera o quadro', () => {
    vi.useFakeTimers()
    const { term, socks } = gridSetup({ readonly: true })
    act(() => socks[0].open())
    act(() => socks[0].msg(new TextEncoder().encode('hist').buffer))
    expect(term.written.map(text)).toEqual(['hist']) // snapshot escrito na hora
    for (const s of ['a', 'b', 'c']) act(() => socks[0].msg(new TextEncoder().encode(s).buffer))
    expect(term.written).toHaveLength(1)
    act(() => { vi.advanceTimersToNextFrame() })
    expect(term.written.map(text)).toEqual(['hist', 'abc'])
  })

  it('a saída que chega antes do exit é escrita antes de onEnded, sem esperar o quadro', () => {
    vi.useFakeTimers()
    const term = new FakeTerm()
    const socks: FakeWs[] = []
    let atEnd: string[] = []
    const onEnded = vi.fn(() => { atEnd = term.written.map(text) })
    render(<TerminalView id="t1" grid readonly termFactory={() => term} wsFactory={(u) => { const w = new FakeWs(u); socks.push(w); return w }} onEnded={onEnded} />)
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => {
      socks[0].msg(new TextEncoder().encode('fim ').buffer)
      socks[0].msg(new TextEncoder().encode('do agente').buffer)
      socks[0].msg(JSON.stringify({ type: 'exit', code: 0, reason: 'agent' }))
    })
    expect(onEnded).toHaveBeenCalledTimes(1)
    expect(atEnd.join('')).toBe('histfim do agente')
  })

  it('a saída pendente é escrita quando a conexão cai, sem esperar o quadro', () => {
    vi.useFakeTimers()
    const { term, socks } = gridSetup({ readonly: true })
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => { socks[0].msg(new TextEncoder().encode('ultima').buffer); socks[0].drop() })
    expect(term.written.map(text).join('')).toBe('histultima')
  })

  describe('com a aba escondida', () => {
    let hidden = false
    beforeEach(() => {
      hidden = false
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
    })
    afterEach(() => { delete (document as { hidden?: boolean }).hidden })

    it('escreve cada mensagem na hora, sem quadro; visível, volta a um write por quadro', () => {
      vi.useFakeTimers()
      const { term, socks } = gridSetup({ readonly: true })
      act(() => socks[0].open())
      snapshot(socks[0], term)
      hidden = true
      act(() => socks[0].msg(new TextEncoder().encode('a').buffer))
      expect(term.written.map(text)).toEqual(['hist', 'a'])
      hidden = false
      for (const s of ['b', 'c']) act(() => socks[0].msg(new TextEncoder().encode(s).buffer))
      expect(term.written).toHaveLength(2)
      act(() => { vi.advanceTimersToNextFrame() })
      expect(term.written.map(text)).toEqual(['hist', 'a', 'bc'])
    })

    it('a saída que esperava o quadro é escrita quando a aba muda de visibilidade', () => {
      vi.useFakeTimers()
      const { term, socks } = gridSetup({ readonly: true })
      act(() => socks[0].open())
      snapshot(socks[0], term)
      act(() => socks[0].msg(new TextEncoder().encode('x').buffer))
      expect(term.written).toHaveLength(1)
      hidden = true
      act(() => { document.dispatchEvent(new Event('visibilitychange')) })
      expect(term.written.map(text)).toEqual(['hist', 'x'])
    })
  })

  it('na grade a entrada continua esperando o snapshot ser escrito', () => {
    vi.useFakeTimers()
    const { term, socks } = gridSetup()
    act(() => socks[0].open())
    act(() => socks[0].msg(new TextEncoder().encode('\x1b[c').buffer))
    term.dataCb('\x1b[?1;2c')
    act(() => { vi.advanceTimersToNextFrame() })
    expect(socks[0].sent).toHaveLength(0)
    act(() => term.flush())
    term.dataCb('ls')
    expect(text(socks[0].sent.at(-1) as Uint8Array)).toBe('ls')
  })

  it('a saída pendente da conexão antiga é descartada ao reconectar', () => {
    vi.useFakeTimers()
    const { term, socks, ref } = gridSetup({ readonly: true })
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => socks[0].msg(new TextEncoder().encode('velho').buffer))
    act(() => ref.current!.reconnect())
    act(() => socks[1].open())
    act(() => { vi.advanceTimersToNextFrame() })
    expect(term.written.map(text)).toEqual(['hist'])
  })

  it('active controla o cursor piscando', () => {
    const { term, rerender } = gridSetup({ readonly: true, active: false })
    expect(term.cursorBlink).toBe(false)
    rerender({ readonly: true, active: true })
    expect(term.cursorBlink).toBe(true)
    rerender({ readonly: true, active: false })
    expect(term.cursorBlink).toBe(false)
  })

  it('assistindo, fit() não ajusta ao contêiner nem manda resize ao PTY', () => {
    vi.useFakeTimers()
    const { term, socks, ref } = gridSetup({ readonly: true })
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 120, rows: 40 })))
    act(() => ref.current!.fit())
    act(() => { vi.advanceTimersByTime(500) })
    expect(term.fits).toBe(0)
    expect(socks[0].sent).toHaveLength(0)
  })

  it('interagindo, fit() ajusta ao contêiner', () => {
    const { term, socks, ref } = gridSetup()
    act(() => socks[0].open())
    snapshot(socks[0], term)
    fireEvent.keyDown(host(), { key: 'a' })
    const before = term.fits
    act(() => ref.current!.fit())
    expect(term.fits).toBe(before + 1)
  })

  it('esgotadas as 3 tentativas avisa desconectado; reconnect() abre uma conexão nova', () => {
    vi.useFakeTimers()
    const onStatus = vi.fn()
    const { socks, ref } = gridSetup({ readonly: true, onStatus })
    expect(onStatus).toHaveBeenLastCalledWith('conectando')
    act(() => socks[0].open())
    expect(onStatus).toHaveBeenLastCalledWith('conectado')
    act(() => socks[0].drop())
    expect(onStatus).toHaveBeenLastCalledWith('reconectando')
    act(() => { vi.advanceTimersByTime(500) }); act(() => socks[1].drop())
    act(() => { vi.advanceTimersByTime(1000) }); act(() => socks[2].drop())
    act(() => { vi.advanceTimersByTime(2000) }); act(() => socks[3].drop())
    act(() => { vi.advanceTimersByTime(60000) })
    expect(socks).toHaveLength(4)
    expect(onStatus).toHaveBeenLastCalledWith('desconectado')
    act(() => ref.current!.reconnect())
    expect(socks).toHaveLength(5)
    expect(socks[4].url).toMatch(/\?readonly=1&batch=1$/)
    expect(onStatus).toHaveBeenLastCalledWith('conectando')
    act(() => socks[4].open())
    expect(onStatus).toHaveBeenLastCalledWith('conectado')
  })

  it('kill() chama o DELETE do terminal', async () => {
    const fetchMock = vi.fn(async () => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const { ref } = gridSetup({ readonly: true })
    await act(async () => { ref.current!.kill() })
    expect(fetchMock).toHaveBeenCalledWith('/api/terminals/t1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('o LazyTerminalView repassa o ref ao TerminalView', async () => {
    const ref = createRef<TerminalHandle>()
    const socks: FakeWs[] = []
    render(<LazyTerminalView ref={ref} id="t1" grid readonly termFactory={() => new FakeTerm()} wsFactory={(u) => { const w = new FakeWs(u); socks.push(w); return w }} />)
    await screen.findByRole('group', { name: 'Terminal do agente' })
    expect(typeof ref.current?.reconnect).toBe('function')
    act(() => ref.current!.reconnect())
    expect(socks).toHaveLength(2)
  })
})

describe('TerminalView: gesto de redimensionar (holdFit/releaseFit)', () => {
  // ResizeObserver simulado (o jsdom não tem): fire() dispara os observers ativos.
  let observers: Set<() => void>
  const fire = () => act(() => { observers.forEach((cb) => cb()) })
  beforeEach(() => {
    observers = new Set()
    vi.stubGlobal('ResizeObserver', class {
      cb: () => void
      constructor(cb: () => void) { this.cb = cb; observers.add(cb) }
      observe() {}
      unobserve() {}
      disconnect() { observers.delete(this.cb) }
    })
  })

  // xterm que, como o real, avisa onResize quando o fit muda as colunas.
  class ResizingTerm extends FakeTerm {
    fit() { super.fit(); this.cols += 1; this.resizeCb() }
  }

  function mount(term: FakeTerm, readonly: boolean) {
    const socks: FakeWs[] = []
    const ref = createRef<TerminalHandle>()
    render(<TerminalView ref={ref} id="t1" grid readonly={readonly} termFactory={() => term} wsFactory={(u) => { const w = new FakeWs(u); socks.push(w); return w }} />)
    return { socks, ref }
  }

  it('assistindo: segurado, o observer não ajusta a fonte; soltar ajusta uma vez e não manda resize', () => {
    vi.useFakeTimers()
    const term = new FakeTerm()
    const { socks, ref } = mount(term, true)
    const h = host()
    // applyFont lê a largura do contêiner uma vez por ajuste: conta as leituras.
    let measures = 0
    let w = 1016
    let hh = 555
    Object.defineProperty(h, 'clientWidth', { configurable: true, get: () => { measures++; return w } })
    Object.defineProperty(h, 'clientHeight', { configurable: true, get: () => hh })
    const xs = document.createElement('div')
    xs.className = 'xterm-screen'
    ;(h.firstElementChild as HTMLElement).appendChild(xs)
    Object.defineProperty(xs, 'offsetWidth', { configurable: true, get: () => 920 * term.fontSize / BASE_FONT })
    Object.defineProperty(xs, 'offsetHeight', { configurable: true, get: () => 680 * term.fontSize / BASE_FONT })
    act(() => socks[0].open())
    snapshot(socks[0], term)
    act(() => socks[0].msg(JSON.stringify({ type: 'info', id: 't1', cols: 120, rows: 40 })))
    act(() => { vi.advanceTimersToNextFrame() })
    expect(term.fontSize).toBe(10)

    measures = 0
    act(() => ref.current!.holdFit())
    w = 2016; hh = 1100 // o tile cresce durante o gesto
    fire(); fire(); fire()
    expect(measures).toBe(0)
    expect(term.fontSize).toBe(10)
    expect(term.fits).toBe(0)

    act(() => ref.current!.releaseFit())
    expect(measures).toBe(1)
    expect(term.fontSize).toBe(BASE_FONT)
    expect(term.fits).toBe(0)
    act(() => { vi.advanceTimersByTime(500) })
    expect(socks[0].sent).toHaveLength(0)

    // Fora do gesto o observer volta a ajustar (−, +, ⤢ e colunas dependem dele).
    fire()
    expect(measures).toBe(2)
  })

  it('interagindo: vários disparos segurados e soltar mandam um único resize ao PTY', () => {
    vi.useFakeTimers()
    const term = new ResizingTerm()
    const { socks, ref } = mount(term, false)
    act(() => socks[0].open())
    snapshot(socks[0], term)
    fireEvent.keyDown(host(), { key: 'a' }) // interage: o PTY passa a seguir o tile
    act(() => { vi.advanceTimersByTime(200) })
    const n = resizes(socks[0]).length
    const f = term.fits
    expect(n).toBeGreaterThan(0)

    act(() => ref.current!.holdFit())
    for (let i = 0; i < 3; i++) {
      fire()
      act(() => { vi.advanceTimersByTime(150) }) // pausa maior que o debounce
    }
    expect(term.fits).toBe(f)
    expect(resizes(socks[0])).toHaveLength(n)

    act(() => ref.current!.releaseFit())
    expect(term.fits).toBe(f + 1)
    act(() => { vi.advanceTimersByTime(150) })
    expect(resizes(socks[0])).toHaveLength(n + 1)
    expect(resizes(socks[0]).at(-1)).toEqual({ type: 'resize', cols: term.cols, rows: term.rows })

    // Sem segurar, o observer ajusta como antes.
    fire()
    expect(term.fits).toBe(f + 2)
    act(() => { vi.advanceTimersByTime(150) })
    expect(resizes(socks[0])).toHaveLength(n + 2)
  })
})

describe('xtermFactory', () => {
  beforeEach(() => {
    webgl.mode = 'ok'; webgl.loseContext = null; webgl.disposed = 0
    // o xterm real consulta matchMedia ao abrir; o jsdom não tem
    vi.stubGlobal('matchMedia', () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }))
  })
  const openReal = (o?: Parameters<typeof xtermFactory>[0]) => {
    const t = xtermFactory(o)
    const el = document.createElement('div')
    document.body.appendChild(el)
    t.open(el)
    return t
  }
  const writes = (t: TermLike, d: string) => new Promise<void>((r) => t.write(d, r))

  it('com webgl usa o renderizador WebGL', () => {
    const t = openReal({ webgl: true })
    expect(t.renderer).toBe('webgl')
    t.dispose()
  })

  it('sem webgl fica no DOM', () => {
    const t = openReal()
    expect(t.renderer).toBe('dom')
    t.dispose()
  })

  it('addon que falha no construtor fica no DOM e write funciona', async () => {
    webgl.mode = 'ctor'
    const t = openReal({ webgl: true })
    expect(t.renderer).toBe('dom')
    await writes(t, 'oi')
    t.dispose()
  })

  it('addon que falha ao ativar (sem WebGL2) fica no DOM e write funciona', async () => {
    webgl.mode = 'activate'
    const t = openReal({ webgl: true })
    expect(t.renderer).toBe('dom')
    await writes(t, 'oi')
    t.dispose()
  })

  it('perda de contexto descarta o addon e volta ao DOM', async () => {
    const t = openReal({ webgl: true })
    expect(t.renderer).toBe('webgl')
    webgl.loseContext!()
    expect(t.renderer).toBe('dom')
    expect(webgl.disposed).toBeGreaterThan(0)
    await writes(t, 'oi')
    t.dispose()
  })

  it('aplica scrollback e cursorBlink das opções e muda cursorBlink depois', () => {
    const t = openReal({ scrollback: 1000, cursorBlink: false })
    expect(t.cursorBlink).toBe(false)
    t.cursorBlink = true
    expect(t.cursorBlink).toBe(true)
    t.dispose()
  })
})
