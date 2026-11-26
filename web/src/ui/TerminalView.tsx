import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { del } from '../api/client'
import { wsUrl } from '../api/terminals'

// TermLike é o mínimo do xterm que a view usa; testes injetam um falso
// (o xterm real precisa de canvas, que o jsdom não tem).
export interface TermLike {
  cols: number
  rows: number
  open(el: HTMLElement): void
  // cb roda depois que o xterm processou os dados (inclusive as respostas a
  // consultas que eles contêm, emitidas por onData).
  write(data: Uint8Array | string, cb?: () => void): void
  resize(cols: number, rows: number): void
  reset(): void
  fit(): void
  // fontSize muda o tamanho da fonte (o xterm recalcula a célula e redesenha).
  fontSize: number
  cursorBlink: boolean
  // renderer: 'webgl' com o addon ativo; 'dom' sem ele ou depois de perder o contexto.
  readonly renderer: 'dom' | 'webgl'
  focus(): void
  onData(cb: (d: string) => void): void
  onResize(cb: () => void): void
  dispose(): void
}

export interface WsLike {
  binaryType: string
  readyState: number
  send(data: string | ArrayBufferLike | Uint8Array): void
  close(): void
  onopen: ((e: unknown) => void) | null
  onmessage: ((e: { data: unknown }) => void) | null
  onclose: ((e: unknown) => void) | null
  onerror: ((e: unknown) => void) | null
}

export interface TermOptions { scrollback?: number; cursorBlink?: boolean; webgl?: boolean }
export type TermFactory = (o?: TermOptions) => TermLike
export type WsFactory = (url: string) => WsLike

export const BASE_FONT = 13
// MIN_FONT: abaixo disso o texto fica ilegível; um PTY ainda maior fica cortado.
export const MIN_FONT = 7

export const THEME = { background: '#0B0D11', foreground: '#E8EAEE', cursor: '#E8EAEE' }

export const xtermFactory: TermFactory = (o = {}) => {
  const t = new Terminal({
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: BASE_FONT,
    cursorBlink: o.cursorBlink ?? true,
    theme: THEME,
    scrollback: o.scrollback ?? 5000,
  })
  const fit = new FitAddon()
  t.loadAddon(fit)
  let renderer: 'dom' | 'webgl' = 'dom'
  // O WebGL só ativa com o xterm já aberto (antes disso a ativação fica para o
  // open, fora do try). Sem WebGL2, ou se o contexto se perde, o addon é
  // descartado e o xterm volta ao renderizador DOM.
  const loadWebgl = () => {
    let addon: WebglAddon | undefined
    try {
      addon = new WebglAddon()
      const a = addon
      a.onContextLoss(() => { renderer = 'dom'; a.dispose() })
      t.loadAddon(a)
      renderer = 'webgl'
    } catch {
      renderer = 'dom'
      try { addon?.dispose() } catch { /* já descartado */ }
    }
  }
  return {
    get cols() { return t.cols },
    get rows() { return t.rows },
    open: (el) => { t.open(el); if (o.webgl) loadWebgl() },
    write: (d, cb) => t.write(d, cb),
    resize: (c, r) => t.resize(c, r),
    reset: () => t.reset(),
    fit: () => { try { fit.fit() } catch { /* contêiner sem tamanho ainda */ } },
    get fontSize() { return t.options.fontSize ?? BASE_FONT },
    set fontSize(n: number) { if (t.options.fontSize !== n) t.options.fontSize = n },
    get cursorBlink() { return t.options.cursorBlink ?? true },
    set cursorBlink(b: boolean) { if (t.options.cursorBlink !== b) t.options.cursorBlink = b },
    get renderer() { return renderer },
    focus: () => t.focus(),
    onData: (cb) => { t.onData(cb) },
    onResize: (cb) => { t.onResize(cb) },
    dispose: () => t.dispose(),
  }
}

const wsFactory: WsFactory = (url) => new WebSocket(url) as unknown as WsLike

// fontToFit é o tamanho de fonte com que um terminal desenhado em
// naturalW×naturalH com a fonte current cabe em availW×availH: seguindo o
// PTY, o xterm tem as linhas e colunas do terminal de origem e pode ser maior
// que o contêiner (que corta o excesso, inclusive as linhas mais novas e o fim
// da barra de rolagem). Fica entre MIN_FONT e BASE_FONT; sem medidas, mantém.
export function fontToFit(current: number, availW: number, availH: number, naturalW: number, naturalH: number): number {
  if (availW <= 0 || availH <= 0 || naturalW <= 0 || naturalH <= 0) return current
  const ratio = Math.min(availW / naturalW, availH / naturalH)
  return Math.max(MIN_FONT, Math.min(BASE_FONT, Math.floor(current * ratio)))
}

const HOST_PADDING = 8

const OPEN = 1
// Na grade: histórico menor (até 9 terminais na tela ao mesmo tempo).
export const GRID_SCROLLBACK = 1000
const MAX_RETRIES = 3
const RESIZE_DEBOUNCE_MS = 100
export const BACKOFF_MS = (attempt: number) => 500 * 2 ** attempt

const btn: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 36, padding: '0 12px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer' }
const btnDanger: React.CSSProperties = { ...btn, borderColor: '#6b2c2c', color: 'var(--err)' }

export type Status = 'conectando' | 'conectado' | 'reconectando' | 'desconectado'

// TerminalHandle é o que o tile da grade comanda pelo ref (menu ⋯ e fim do
// gesto de redimensionar).
export interface TerminalHandle {
  setReadonly(ro: boolean): void
  kill(): void
  fit(): void
  reconnect(): void
}

export interface TerminalViewProps {
  id: string
  readonly?: boolean
  autoFocus?: boolean
  termFactory?: TermFactory
  wsFactory?: WsFactory
  onEnded?: () => void
  // grid: modo grade — scrollback menor, WebGL, uma escrita por quadro,
  // batch=1 enquanto só assiste e sem a barra de botões própria (o tile a faz).
  grid?: boolean
  // active: na grade, o cursor só pisca no terminal ativo.
  active?: boolean
  // onMode avisa a troca entre assistir (true) e interagir (false).
  onMode?: (ro: boolean) => void
  // onStatus avisa o estado da conexão (na grade é o tile que o mostra).
  onStatus?: (s: Status) => void
}

// Quem só assiste nunca muda o PTY: o xterm segue o tamanho dele (mensagem
// info) e nenhum resize é enviado até o usuário interagir (clicar em
// "Interagir" ou digitar no modo interativo). Assim abrir a aba não encolhe o
// terminal de quem está no `nobre run`. A entrada só vale depois que o
// snapshot foi escrito: as respostas do xterm a consultas antigas (DA, CPR,
// OSC) no histórico não viram teclas para o agente.

export const TerminalView = forwardRef<TerminalHandle, TerminalViewProps>(function TerminalView({
  id, readonly = false, autoFocus = false, termFactory = xtermFactory, wsFactory: makeWs = wsFactory, onEnded,
  grid = false, active = false, onMode, onStatus,
}, ref) {
  const host = useRef<HTMLDivElement>(null)
  const screenRef = useRef<HTMLDivElement>(null)
  const [ro, setRo] = useState(readonly)
  const [status, setStatus] = useState<Status>('conectando')
  const [exitCode, setExitCode] = useState<number | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [killError, setKillError] = useState('')
  const [killing, setKilling] = useState(false)
  const endedCb = useRef(onEnded)
  endedCb.current = onEnded
  const modeCb = useRef(onMode)
  modeCb.current = onMode
  const statusCb = useRef(onStatus)
  statusCb.current = onStatus
  const activeRef = useRef(active)
  activeRef.current = active
  // engaged: o usuário interagiu; só então o PTY segue o tamanho desta view.
  const engaged = useRef(false)
  const termRef = useRef<TermLike | null>(null)
  // Ações que dependem da conexão viva, preenchidas pelo efeito principal.
  const live = useRef<{ fit(): void; reconnect(): void } | null>(null)

  useEffect(() => { setRo(readonly) }, [readonly, id])
  useEffect(() => { setExitCode(null); setConfirming(false); setKillError(''); engaged.current = false }, [id])

  // onMode só na troca, não no modo inicial.
  const lastRo = useRef(ro)
  useEffect(() => {
    if (lastRo.current === ro) return
    lastRo.current = ro
    modeCb.current?.(ro)
  }, [ro])
  useEffect(() => { statusCb.current?.(status) }, [status])

  useEffect(() => {
    const el = host.current
    const scr = screenRef.current
    if (!el || !scr) return
    const term = grid ? termFactory({ scrollback: GRID_SCROLLBACK, webgl: true, cursorBlink: activeRef.current }) : termFactory()
    termRef.current = term
    term.open(scr)
    let ws: WsLike | null = null
    let disposed = false
    let exited = false
    let attempt = 0
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let resizeTimer: ReturnType<typeof setTimeout> | undefined
    let ready = false // snapshot desta conexão já escrito
    let gotSnapshot = false

    const followPty = () => ro || !engaged.current
    // Seguindo o PTY o xterm não é ajustado ao contêiner: diminui a fonte para
    // as linhas e colunas do PTY caberem inteiras (sem transform, o mouse e a
    // seleção continuam alinhados). Fora disso a fonte volta à base.
    const applyFont = () => {
      if (!followPty()) { term.fontSize = BASE_FONT; return }
      const natural = scr.querySelector<HTMLElement>('.xterm-screen') ?? scr
      const next = fontToFit(term.fontSize, el.clientWidth - 2 * HOST_PADDING, el.clientHeight - 2 * HOST_PADDING, natural.offsetWidth, natural.offsetHeight)
      if (next !== term.fontSize) term.fontSize = next
    }
    let raf = 0
    const refont = () => {
      applyFont()
      // a célula muda depois do redesenho: confere de novo no próximo quadro
      if (typeof requestAnimationFrame === 'function') {
        cancelAnimationFrame(raf)
        raf = requestAnimationFrame(() => { if (!disposed) applyFont() })
      }
    }
    const sendResize = () => {
      if (followPty() || !ws || ws.readyState !== OPEN) return
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
    // Captura: roda antes do xterm transformar a tecla em onData, então o
    // resize chega ao PTY antes da primeira tecla.
    const engage = () => {
      if (ro || engaged.current) return
      engaged.current = true
      applyFont()
      term.fit()
      sendResize()
    }
    el.addEventListener('keydown', engage, true)
    el.addEventListener('paste', engage, true)
    term.onData((d) => {
      if (ro || !ready || !ws || ws.readyState !== OPEN) return
      ws.send(new TextEncoder().encode(d))
    })
    term.onResize(() => {
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(sendResize, RESIZE_DEBOUNCE_MS)
    })
    // Na grade a saída que chega entre dois quadros vira um único write: com
    // vários terminais na tela, um write por mensagem trava o desenho.
    let pending: Uint8Array[] = []
    let outFrame = 0
    const flushOut = () => {
      outFrame = 0
      const chunks = pending
      pending = []
      if (chunks.length === 0) return
      if (chunks.length === 1) { term.write(chunks[0]); return }
      const all = new Uint8Array(chunks.reduce((n, c) => n + c.byteLength, 0))
      let off = 0
      for (const c of chunks) { all.set(c, off); off += c.byteLength }
      term.write(all)
    }
    const dropOut = () => {
      pending = []
      if (outFrame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(outFrame)
      outFrame = 0
    }
    const queueOut = (bytes: Uint8Array) => {
      if (!grid || typeof requestAnimationFrame !== 'function') { term.write(bytes); return }
      pending.push(bytes)
      if (!outFrame) outFrame = requestAnimationFrame(flushOut)
    }
    // O snapshot nunca espera o quadro: a entrada só vale depois dele escrito.
    const writeOut = (bytes: Uint8Array, sock: WsLike) => {
      if (gotSnapshot) { queueOut(bytes); return }
      gotSnapshot = true // a primeira mensagem binária é o snapshot (pode ser vazio)
      if (bytes.byteLength === 0) { ready = true; return }
      term.write(bytes, () => { if (ws === sock) ready = true })
    }

    const connect = () => {
      const sock = makeWs(wsUrl(id, ro, undefined, grid && ro))
      ws = sock
      sock.binaryType = 'arraybuffer'
      sock.onopen = () => {
        attempt = 0
        ready = false
        gotSnapshot = false
        dropOut() // a saída pendente era da conexão anterior
        setStatus('conectado')
        term.reset() // o servidor reenvia o snapshot a cada conexão
        applyFont() // ao alternar o modo a fonte volta à base (ou encolhe)
        if (!followPty()) {
          term.fit()
          sendResize()
        }
        if (!ro && autoFocus) term.focus()
      }
      sock.onmessage = (e) => {
        const d = e.data
        if (typeof d === 'string') {
          try {
            const m = JSON.parse(d) as { type?: string; code?: number; cols?: number; rows?: number }
            if (m.type === 'info' && followPty() && m.cols && m.rows) {
              term.resize(m.cols, m.rows)
              refont()
            }
            if (m.type === 'exit') {
              exited = true
              setExitCode(typeof m.code === 'number' ? m.code : 0)
              endedCb.current?.()
            }
          } catch { /* texto desconhecido */ }
          return
        }
        if (Object.prototype.toString.call(d) === '[object ArrayBuffer]') writeOut(new Uint8Array(d as ArrayBuffer), sock)
        else if (ArrayBuffer.isView(d)) writeOut(new Uint8Array(d.buffer, d.byteOffset, d.byteLength), sock)
      }
      sock.onclose = () => {
        if (disposed || ws !== sock) return
        if (exited) { setStatus('desconectado'); return }
        if (attempt >= MAX_RETRIES) { setStatus('desconectado'); return }
        setStatus('reconectando')
        retryTimer = setTimeout(connect, BACKOFF_MS(attempt))
        attempt++
      }
    }
    setStatus('conectando')
    connect()

    // reconnect abre uma conexão nova (o tile oferece "Tentar de novo").
    const reconnect = () => {
      clearTimeout(retryTimer)
      if (ws) { ws.onclose = null; ws.onmessage = null; ws.onopen = null; ws.close() }
      dropOut()
      attempt = 0
      setStatus('conectando')
      connect()
    }

    const onWinResize = () => { if (followPty()) applyFont(); else term.fit() }
    live.current = { fit: onWinResize, reconnect }
    window.addEventListener('resize', onWinResize)
    const RO = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(onWinResize)
    RO?.observe(el)
    RO?.observe(scr)

    return () => {
      disposed = true
      clearTimeout(retryTimer)
      clearTimeout(resizeTimer)
      window.removeEventListener('resize', onWinResize)
      el.removeEventListener('keydown', engage, true)
      el.removeEventListener('paste', engage, true)
      RO?.disconnect()
      if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(raf)
      dropOut()
      if (ws) { ws.onclose = null; ws.onmessage = null; ws.onopen = null; ws.close() }
      live.current = null
      termRef.current = null
      term.dispose()
    }
  }, [id, ro, autoFocus, termFactory, makeWs, grid])

  // Depois do efeito principal: no primeiro render o terminal já existe.
  useEffect(() => { if (grid && termRef.current) termRef.current.cursorBlink = active }, [grid, active])

  const kill = async () => {
    setKilling(true); setKillError('')
    try {
      await del(`/api/terminals/${encodeURIComponent(id)}`)
      setConfirming(false)
    } catch (e) {
      setKillError(e instanceof Error ? e.message : 'não consegui encerrar o agente')
    } finally {
      setKilling(false)
    }
  }

  // setReadonly(false) vale como "Interagir": o PTY passa a seguir esta view.
  const toggleMode = (next: boolean) => { engaged.current = !next; setRo(next) }
  useImperativeHandle(ref, () => ({
    setReadonly: (next) => { if (next !== ro) toggleMode(next) },
    kill: () => { void kill() },
    fit: () => live.current?.fit(),
    reconnect: () => live.current?.reconnect(),
  }))

  const ended = exitCode !== null
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, height: '100%', minHeight: 0 }}>
      {!grid && <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
        <span role="status" style={{ fontSize: 13, color: ended ? 'var(--warn)' : 'var(--text-2)', flex: '1 1 auto' }}>
          {ended ? `encerrado (código ${exitCode})` : status}
        </span>
        {!ended && (
          <button type="button" style={btn} aria-pressed={ro} onClick={() => toggleMode(!ro)}>
            {ro ? 'Interagir' : 'Somente assistir'}
          </button>
        )}
        {!ended && !confirming && (
          <button type="button" style={btnDanger} onClick={() => setConfirming(true)}>Encerrar agente</button>
        )}
        {!ended && confirming && (
          <span role="group" aria-label="Confirmar encerramento" style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
            <span style={{ fontSize: 13 }}>Encerrar o agente e o terminal?</span>
            <button type="button" style={btnDanger} disabled={killing} onClick={kill}>Sim, encerrar</button>
            <button type="button" style={btn} disabled={killing} onClick={() => setConfirming(false)}>Cancelar</button>
          </span>
        )}
      </div>}
      {killError && <p role="alert" style={{ margin: 0, fontSize: 13, color: 'var(--err)' }}>{killError}</p>}
      <div
        ref={host}
        role="group"
        aria-label="Terminal do agente"
        style={{ flex: '1 1 auto', minHeight: grid ? 0 : 240, padding: HOST_PADDING, borderRadius: 8, background: THEME.background, border: '1px solid var(--border)', overflow: 'hidden' }}
      >
        {/* Ocupa o contêiner: o FitAddon mede o pai do xterm (este div). */}
        <div ref={screenRef} data-testid="terminal-screen" style={{ width: '100%', height: '100%' }} />
      </div>
    </div>
  )
})
