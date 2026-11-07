import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
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

export type TermFactory = () => TermLike
export type WsFactory = (url: string) => WsLike

export const THEME = { background: '#0B0D11', foreground: '#E8EAEE', cursor: '#E8EAEE' }

export const xtermFactory: TermFactory = () => {
  const t = new Terminal({
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: 13,
    cursorBlink: true,
    theme: THEME,
    scrollback: 5000,
  })
  const fit = new FitAddon()
  t.loadAddon(fit)
  return {
    get cols() { return t.cols },
    get rows() { return t.rows },
    open: (el) => t.open(el),
    write: (d, cb) => t.write(d, cb),
    resize: (c, r) => t.resize(c, r),
    reset: () => t.reset(),
    fit: () => { try { fit.fit() } catch { /* contêiner sem tamanho ainda */ } },
    focus: () => t.focus(),
    onData: (cb) => { t.onData(cb) },
    onResize: (cb) => { t.onResize(cb) },
    dispose: () => t.dispose(),
  }
}

const wsFactory: WsFactory = (url) => new WebSocket(url) as unknown as WsLike

const OPEN = 1
const MAX_RETRIES = 3
const RESIZE_DEBOUNCE_MS = 100
export const BACKOFF_MS = (attempt: number) => 500 * 2 ** attempt

const btn: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 36, padding: '0 12px', borderRadius: 8, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer' }
const btnDanger: React.CSSProperties = { ...btn, borderColor: '#6b2c2c', color: 'var(--err)' }

type Status = 'conectando' | 'conectado' | 'reconectando' | 'desconectado'

// Quem só assiste nunca muda o PTY: o xterm segue o tamanho dele (mensagem
// info) e nenhum resize é enviado até o usuário interagir (clicar em
// "Interagir" ou digitar no modo interativo). Assim abrir a aba não encolhe o
// terminal de quem está no `nobre run`. A entrada só vale depois que o
// snapshot foi escrito: as respostas do xterm a consultas antigas (DA, CPR,
// OSC) no histórico não viram teclas para o agente.

export function TerminalView({
  id, readonly = false, autoFocus = false, termFactory = xtermFactory, wsFactory: makeWs = wsFactory, onEnded,
}: {
  id: string
  readonly?: boolean
  autoFocus?: boolean
  termFactory?: TermFactory
  wsFactory?: WsFactory
  onEnded?: () => void
}) {
  const host = useRef<HTMLDivElement>(null)
  const [ro, setRo] = useState(readonly)
  const [status, setStatus] = useState<Status>('conectando')
  const [exitCode, setExitCode] = useState<number | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [killError, setKillError] = useState('')
  const [killing, setKilling] = useState(false)
  const endedCb = useRef(onEnded)
  endedCb.current = onEnded
  // engaged: o usuário interagiu; só então o PTY segue o tamanho desta view.
  const engaged = useRef(false)

  useEffect(() => { setRo(readonly) }, [readonly, id])
  useEffect(() => { setExitCode(null); setConfirming(false); setKillError(''); engaged.current = false }, [id])

  useEffect(() => {
    const el = host.current
    if (!el) return
    const term = termFactory()
    term.open(el)
    let ws: WsLike | null = null
    let disposed = false
    let exited = false
    let attempt = 0
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let resizeTimer: ReturnType<typeof setTimeout> | undefined
    let ready = false // snapshot desta conexão já escrito
    let gotSnapshot = false

    const followPty = () => ro || !engaged.current
    const sendResize = () => {
      if (followPty() || !ws || ws.readyState !== OPEN) return
      ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
    }
    // Captura: roda antes do xterm transformar a tecla em onData, então o
    // resize chega ao PTY antes da primeira tecla.
    const engage = () => {
      if (ro || engaged.current) return
      engaged.current = true
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
    const writeOut = (bytes: Uint8Array, sock: WsLike) => {
      if (gotSnapshot) { term.write(bytes); return }
      gotSnapshot = true // a primeira mensagem binária é o snapshot (pode ser vazio)
      if (bytes.byteLength === 0) { ready = true; return }
      term.write(bytes, () => { if (ws === sock) ready = true })
    }

    const connect = () => {
      const sock = makeWs(wsUrl(id, ro))
      ws = sock
      sock.binaryType = 'arraybuffer'
      sock.onopen = () => {
        attempt = 0
        ready = false
        gotSnapshot = false
        setStatus('conectado')
        term.reset() // o servidor reenvia o snapshot a cada conexão
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
            if (m.type === 'info' && followPty() && m.cols && m.rows) term.resize(m.cols, m.rows)
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

    const onWinResize = () => { if (!followPty()) term.fit() }
    window.addEventListener('resize', onWinResize)
    const RO = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(onWinResize)
    RO?.observe(el)

    return () => {
      disposed = true
      clearTimeout(retryTimer)
      clearTimeout(resizeTimer)
      window.removeEventListener('resize', onWinResize)
      el.removeEventListener('keydown', engage, true)
      el.removeEventListener('paste', engage, true)
      RO?.disconnect()
      if (ws) { ws.onclose = null; ws.onmessage = null; ws.onopen = null; ws.close() }
      term.dispose()
    }
  }, [id, ro, autoFocus, termFactory, makeWs])

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

  const ended = exitCode !== null
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, height: '100%', minHeight: 0 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
        <span role="status" style={{ fontSize: 13, color: ended ? 'var(--warn)' : 'var(--text-2)', flex: '1 1 auto' }}>
          {ended ? `encerrado (código ${exitCode})` : status}
        </span>
        {!ended && (
          <button type="button" style={btn} aria-pressed={ro} onClick={() => { engaged.current = ro; setRo(!ro) }}>
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
      </div>
      {killError && <p role="alert" style={{ margin: 0, fontSize: 13, color: 'var(--err)' }}>{killError}</p>}
      <div
        ref={host}
        role="group"
        aria-label="Terminal do agente"
        style={{ flex: '1 1 auto', minHeight: 240, padding: 8, borderRadius: 8, background: THEME.background, border: '1px solid var(--border)', overflow: 'hidden' }}
      />
    </div>
  )
}
