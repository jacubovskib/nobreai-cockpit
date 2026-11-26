// Bancada de performance da grade de terminais (só em desenvolvimento:
// `pnpm bench`). Monta 9 tiles com o TerminalView real e o xterm real, alimenta
// cada um com saída sintética e mede a thread principal. A conexão é falsa, mas
// fala o protocolo do daemon: info, snapshot binário, saída binária, exit.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { TerminalView, xtermFactory, type TermLike, type TermFactory, type WsFactory, type WsLike } from '../src/ui/TerminalView'
import { percentile, tick, verdict, type Profile } from './load'
import type { FromWorker, ToWorker } from './worker'

const TILES = 9
const WARMUP_MS = 1000
const MEASURE_MS = 6000
const REPLUG_MS = 3000
const SNAPSHOT_BYTES = 2 * 1024 * 1024
const LAG_STEP_MS = 10
const MIN_FPS = 5

type Mode = Profile | 'religar'

export interface BenchResult {
  profile: Mode
  fps: number
  longCount: number
  longSum: number
  longMax: number
  lagP95: number
  lagMax: number
  renderers: ('dom' | 'webgl')[]
  visibility: DocumentVisibilityState
  verdict: 'aprovado' | 'reprovado' | null // null: a religação não entra no veredito
}

declare global {
  interface Window { __bench?: BenchResult | { running: true } }
}

const ids = Array.from({ length: TILES }, (_, i) => `bench-${i}`)

// Registro das conexões falsas, por id do tile.
const sockets = new Map<string, FakeSocket>()
let snapshotBytes = 0

class FakeSocket implements WsLike {
  binaryType = 'blob'
  readyState = 0
  onopen: WsLike['onopen'] = null
  onmessage: WsLike['onmessage'] = null
  onclose: WsLike['onclose'] = null
  onerror: WsLike['onerror'] = null
  constructor(readonly id: string) {
    sockets.set(id, this)
    // o componente atribui os handlers logo depois do construtor
    setTimeout(() => {
      if (this.readyState !== 0) return
      this.readyState = 1
      this.onopen?.({})
      this.onmessage?.({ data: JSON.stringify({ type: 'info', cols: 120, rows: 30 }) })
      this.onmessage?.({ data: snapshot(snapshotBytes) })
    }, 0)
  }
  send() { /* ninguém digita na bancada */ }
  close() {
    this.readyState = 3
    if (sockets.get(this.id) === this) sockets.delete(this.id)
  }
  push(buf: ArrayBuffer) {
    if (this.readyState === 1) this.onmessage?.({ data: buf })
  }
}

// snapshot monta ~n bytes de histórico (vazio com n = 0).
function snapshot(n: number): ArrayBuffer {
  const out = new Uint8Array(n)
  let off = 0
  for (let k = 0; off < n; k++) {
    const b = tick('enxurrada', k)
    const len = Math.min(b.byteLength, n - off)
    out.set(b.subarray(0, len), off)
    off += len
  }
  return out.buffer
}

const benchWs: WsFactory = (url) => {
  const m = /\/terminals\/([^/]+)\/ws/.exec(url)
  return new FakeSocket(decodeURIComponent(m?.[1] ?? ''))
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

// Mede por `ms`: quadros por segundo (rAF), tarefas longas (PerformanceObserver)
// e atraso da thread principal (um timer de 10 ms que mede o quanto chega tarde).
async function measure(ms: number) {
  const longs: number[] = []
  let po: PerformanceObserver | null = null
  try {
    po = new PerformanceObserver((l) => { for (const e of l.getEntries()) longs.push(e.duration) })
    po.observe({ type: 'longtask', buffered: false })
  } catch { /* sem suporte a longtask: longCount fica 0 */ }
  let frames = 0
  let raf = requestAnimationFrame(function f() { frames++; raf = requestAnimationFrame(f) })
  const lags: number[] = []
  const t0 = performance.now()
  let last = t0
  while (performance.now() - t0 < ms) {
    await sleep(LAG_STEP_MS)
    const now = performance.now()
    lags.push(Math.max(0, now - last - LAG_STEP_MS))
    last = now
  }
  const elapsed = (performance.now() - t0) / 1000
  cancelAnimationFrame(raf)
  // entrega o que o observer ainda tem na fila
  if (po) { longs.push(...po.takeRecords().map((e) => e.duration)); po.disconnect() }
  return {
    fps: frames / elapsed,
    longCount: longs.length,
    longSum: longs.reduce((a, b) => a + b, 0),
    longMax: Math.max(0, ...longs),
    lagP95: percentile(lags, 95),
    lagMax: Math.max(0, ...lags),
  }
}

const fmt = (n: number) => n.toFixed(1)

function Bench() {
  const [gen, setGen] = useState(0) // muda a key dos tiles: desmonta e remonta
  const [busy, setBusy] = useState<string | null>(null)
  const [result, setResult] = useState<BenchResult | null>(null)
  const terms = useRef<(TermLike | null)[]>([])
  const worker = useRef<Worker | null>(null)

  // Uma fábrica estável por tile: o TerminalView recria o terminal se ela mudar.
  const factories = useMemo<TermFactory[]>(() => ids.map((_, i) => (o) => {
    const t = xtermFactory(o)
    terms.current[i] = t
    return t
  }), [])

  const startLoad = useCallback((p: Profile) => {
    const w = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module' })
    w.onmessage = (e: MessageEvent<FromWorker>) => {
      e.data.chunks.forEach((c, i) => { if (c) sockets.get(ids[i])?.push(c) })
    }
    w.postMessage({ cmd: 'start', profile: p, tiles: TILES } satisfies ToWorker)
    worker.current = w
  }, [])
  const stopLoad = useCallback(() => { worker.current?.terminate(); worker.current = null }, [])

  const finish = useCallback((profile: Mode, m: Awaited<ReturnType<typeof measure>>) => {
    const r: BenchResult = {
      ...m,
      profile,
      renderers: terms.current.map((t) => t?.renderer ?? 'dom'),
      visibility: document.visibilityState,
      verdict: profile === 'religar' ? null : verdict(m),
    }
    window.__bench = r
    setResult(r)
  }, [])

  const run = useCallback(async (mode: Mode) => {
    if (busy) return
    setBusy(mode)
    setResult(null)
    window.__bench = { running: true }
    try {
      if (mode === 'religar') {
        // desmonta os 9 e remonta cada um com um snapshot de 2 MiB
        snapshotBytes = 0
        setGen((g) => g + 1)
        await sleep(500)
        snapshotBytes = SNAPSHOT_BYTES
        const pending = measure(REPLUG_MS)
        setGen((g) => g + 1)
        finish('religar', await pending)
        snapshotBytes = 0
      } else {
        startLoad(mode)
        await sleep(WARMUP_MS)
        finish(mode, await measure(MEASURE_MS))
        stopLoad()
      }
    } finally {
      stopLoad()
      setBusy(null)
    }
  }, [busy, finish, startLoad, stopLoad])

  // ?auto=agente|enxurrada|religar começa a rodada sem clique
  const auto = useRef(false)
  useEffect(() => {
    if (auto.current) return
    auto.current = true
    const a = new URLSearchParams(location.search).get('auto')
    if (a === 'agente' || a === 'enxurrada' || a === 'religar') {
      // espera os tiles abrirem (o WebGL só liga com o xterm aberto)
      setTimeout(() => { void run(a) }, 500)
    }
  }, [run])

  const hidden = document.visibilityState !== 'visible'
  const lowFps = result !== null && result.fps < MIN_FPS
  const btn = { padding: '8px 16px', borderRadius: 8, border: '1px solid #3A3F4B', background: '#1a1d24', color: '#E8EAEE', font: 'inherit', cursor: busy ? 'wait' : 'pointer' } as const

  return (
    <div style={{ padding: 16 }}>
      <h1 style={{ fontSize: 18, margin: '0 0 12px' }}>Bancada de performance dos terminais</h1>
      <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
        <button style={btn} disabled={!!busy} onClick={() => void run('agente')}>Agente</button>
        <button style={btn} disabled={!!busy} onClick={() => void run('enxurrada')}>Enxurrada</button>
        <button style={btn} disabled={!!busy} onClick={() => void run('religar')}>Religar</button>
      </div>
      {(hidden || lowFps) && (
        <p role="alert" style={{ color: '#f0b429', margin: '0 0 12px' }}>
          {hidden ? 'A janela não está visível: ' : `Só ${fmt(result!.fps)} quadros por segundo: `}
          a medição não vale. Deixe a janela à frente, sem nada por cima, e rode de novo.
        </p>
      )}
      <div role="status" style={{ marginBottom: 12, fontSize: 14, lineHeight: 1.6 }}>
        {busy && <div>Rodando {busy}… ({busy === 'religar' ? `${REPLUG_MS / 1000} s` : `${WARMUP_MS / 1000} s de aquecimento, ${MEASURE_MS / 1000} s de medição`})</div>}
        {!busy && !result && <div>Escolha um perfil para medir.</div>}
        {result && !busy && (
          <>
            <div>Perfil: <b>{result.profile}</b>{result.profile === 'religar' && ' (custo à parte, fora do veredito)'}</div>
            <div>Quadros por segundo: {fmt(result.fps)}</div>
            <div>Tarefas longas: {result.longCount} (soma {fmt(result.longSum)} ms, máxima {fmt(result.longMax)} ms)</div>
            <div>Atraso da thread principal: p95 {fmt(result.lagP95)} ms, máximo {fmt(result.lagMax)} ms</div>
            <div>Renderizador: {result.renderers.map((r, i) => `${i + 1}:${r}`).join(' ')}</div>
            <div>Visibilidade: {result.visibility}</div>
            {result.verdict && <div style={{ fontSize: 18, color: result.verdict === 'aprovado' ? '#3ecf8e' : '#f4676b' }}>Veredito: {result.verdict}</div>}
          </>
        )}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 440px)', gap: 8 }}>
        {ids.map((id, i) => (
          <div key={`${id}-${gen}`} style={{ width: 440, height: 300 }}>
            <TerminalView id={id} grid readonly wsFactory={benchWs} termFactory={factories[i]} />
          </div>
        ))}
      </div>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(<Bench />)
