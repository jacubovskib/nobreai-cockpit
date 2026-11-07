import '@testing-library/jest-dom/vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { TerminalView, type TermLike, type WsLike } from './TerminalView'

class FakeTerm implements TermLike {
  cols = 100
  rows = 30
  written: Uint8Array[] = []
  resets = 0
  disposed = false
  dataCb: (d: string) => void = () => {}
  open() {}
  write(d: Uint8Array | string) { this.written.push(typeof d === 'string' ? new TextEncoder().encode(d) : d) }
  reset() { this.resets++ }
  fit() {}
  focus() {}
  onData(cb: (d: string) => void) { this.dataCb = cb }
  onResize() {}
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

describe('TerminalView', () => {
  it('conecta no ws do terminal, escreve bytes binários e envia resize ao abrir', () => {
    const { term, socks } = setup()
    expect(socks[0].url).toBe(`ws://${window.location.host}/api/terminals/t1/ws`)
    act(() => socks[0].open())
    expect(JSON.parse(socks[0].sent[0] as string)).toEqual({ type: 'resize', cols: 100, rows: 30 })
    act(() => socks[0].msg(new TextEncoder().encode('olá').buffer))
    expect(text(term.written[0])).toBe('olá')
  })

  it('envia teclas como binário e, em somente assistir, não envia nada e usa ?readonly=1', () => {
    const a = setup()
    act(() => a.socks[0].open())
    a.term.dataCb('ls')
    const last = a.socks[0].sent.at(-1) as Uint8Array
    expect(text(last)).toBe('ls')
    a.unmount()

    const b = setup({ readonly: true })
    expect(b.socks[0].url).toMatch(/\?readonly=1$/)
    act(() => b.socks[0].open())
    b.term.dataCb('x')
    expect(b.socks[0].sent).toHaveLength(0)
  })

  it('o botão alterna entre somente assistir e interagir reconectando', () => {
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
    act(() => socks[0].msg(JSON.stringify({ type: 'exit', code: 0 })))
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

  it('Encerrar agente pede confirmação inline e chama DELETE', async () => {
    const fetchMock = vi.fn(async () => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    setup()
    fireEvent.click(screen.getByRole('button', { name: 'Encerrar agente' }))
    expect(fetchMock).not.toHaveBeenCalled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Sim, encerrar' })) })
    expect(fetchMock).toHaveBeenCalledWith('/api/terminals/t1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('fecha o socket e descarta o terminal ao desmontar', () => {
    const { socks, term, unmount } = setup()
    unmount()
    expect(socks[0].closed).toBe(true)
    expect(term.disposed).toBe(true)
  })
})
